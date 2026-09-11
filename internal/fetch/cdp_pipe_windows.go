//go:build windows

package fetch

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// pipeChrome — короткий CDP-сеанс только чтобы поставить расширение.
// Магазинные URL в этом процессе не открываем: pipe = автоматизация,
// Ozon/DNS тогда показывают заглушку или 403.
type pipeChrome struct {
	cmd     *exec.Cmd
	to      io.WriteCloser
	from    io.ReadCloser
	mu      sync.Mutex
	nextID  int
	pending map[int]chan cdpMsg
	closed  chan struct{}
}

type cdpMsg struct {
	ID     int             `json:"id"`
	Error  *cdpErr         `json:"error"`
	Result json.RawMessage `json:"result"`
	Method string          `json:"method"`
}

type cdpErr struct {
	Message string `json:"message"`
}

func installUnpackedToProfile(exe, profile, extDir string) (string, error) {
	extDir, err := filepath.Abs(extDir)
	if err != nil {
		return "", err
	}
	killChromeWithProfile(profile)
	clearStaleProfileLocks(profile)
	markChromeExitedCleanly(profile)

	pc, err := startPipeChrome(exe, profile)
	if err != nil {
		return "", err
	}
	defer func() {
		pc.close()
		killChromeWithProfile(profile)
		clearStaleProfileLocks(profile)
		markChromeExitedCleanly(profile)
	}()

	id, err := pc.persistViaExtensionsPage(extDir)
	if err != nil {
		slog.Default().Warn("не удалось поставить расширение через chrome://extensions", "error", err)
		id, err = pc.loadUnpacked(extDir, 15*time.Second)
		if err != nil {
			return "", err
		}
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if profileMentionsExtension(profile, extDir) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	_, _ = pc.call("Browser.close", map[string]any{}, 5*time.Second)
	pc.waitExit(10 * time.Second)
	if id == "" {
		return "", fmt.Errorf("chrome: не получили id расширения")
	}
	return id, nil
}

func startPipeChrome(exe, profile string) (*pipeChrome, error) {
	toChildR, toChildW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	fromChildR, fromChildW, err := os.Pipe()
	if err != nil {
		toChildR.Close()
		toChildW.Close()
		return nil, err
	}

	childRead, err := inheritableDup(windows.Handle(toChildR.Fd()))
	if err != nil {
		closePipes(toChildR, toChildW, fromChildR, fromChildW)
		return nil, err
	}
	childWrite, err := inheritableDup(windows.Handle(fromChildW.Fd()))
	if err != nil {
		windows.CloseHandle(childRead)
		closePipes(toChildR, toChildW, fromChildR, fromChildW)
		return nil, err
	}

	inH, outH := uint32(childRead), uint32(childWrite)
	if uintptr(inH) != uintptr(childRead) || uintptr(outH) != uintptr(childWrite) {
		windows.CloseHandle(childRead)
		windows.CloseHandle(childWrite)
		closePipes(toChildR, toChildW, fromChildR, fromChildW)
		return nil, fmt.Errorf("chrome pipe: слишком большой handle")
	}

	args := make([]string, 0, len(installChromeArgs(profile))+1)
	for _, a := range installChromeArgs(profile) {
		args = append(args, a)
		if a == "--remote-debugging-pipe" {
			args = append(args, fmt.Sprintf("--remote-debugging-io-pipes=%d,%d", inH, outH))
		}
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		AdditionalInheritedHandles: []syscall.Handle{
			syscall.Handle(childRead),
			syscall.Handle(childWrite),
		},
	}
	if err := cmd.Start(); err != nil {
		windows.CloseHandle(childRead)
		windows.CloseHandle(childWrite)
		closePipes(toChildR, toChildW, fromChildR, fromChildW)
		return nil, fmt.Errorf("chrome pipe: %w", err)
	}
	_ = toChildR.Close()
	_ = fromChildW.Close()
	_ = windows.CloseHandle(childRead)
	_ = windows.CloseHandle(childWrite)

	pc := &pipeChrome{
		cmd:     cmd,
		to:      toChildW,
		from:    fromChildR,
		pending: map[int]chan cdpMsg{},
		closed:  make(chan struct{}),
	}
	go pc.readLoop()
	return pc, nil
}

func (pc *pipeChrome) close() {
	select {
	case <-pc.closed:
	default:
		close(pc.closed)
	}
	if pc.to != nil {
		_ = pc.to.Close()
	}
	if pc.from != nil {
		_ = pc.from.Close()
	}
	if pc.cmd != nil && pc.cmd.Process != nil {
		_ = pc.cmd.Process.Kill()
	}
}

func (pc *pipeChrome) waitExit(d time.Duration) {
	if pc.cmd == nil || pc.cmd.Process == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_ = pc.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
		_ = pc.cmd.Process.Kill()
		<-done
	}
}

func (pc *pipeChrome) persistViaExtensionsPage(extDir string) (string, error) {
	raw, err := pc.call("Target.createTarget", map[string]any{"url": "chrome://extensions"}, 8*time.Second)
	if err != nil {
		return "", err
	}
	var created struct {
		TargetID string `json:"targetId"`
	}
	if json.Unmarshal(raw, &created) != nil || created.TargetID == "" {
		return "", fmt.Errorf("Target.createTarget: нет targetId")
	}
	raw, err = pc.call("Target.attachToTarget", map[string]any{"targetId": created.TargetID, "flatten": true}, 8*time.Second)
	if err != nil {
		return "", err
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(raw, &attached) != nil || attached.SessionID == "" {
		return "", fmt.Errorf("Target.attachToTarget: нет sessionId")
	}
	time.Sleep(2 * time.Second)
	pathJSON, err := json.Marshal(extDir)
	if err != nil {
		return "", err
	}
	expr := `(async () => {
  const path = ` + string(pathJSON) + `;
  const wrap = (fn) => new Promise((resolve, reject) => {
    fn((result) => {
      const err = (chrome.runtime && chrome.runtime.lastError) ? chrome.runtime.lastError.message : '';
      if (err) reject(new Error(err));
      else resolve(result);
    });
  });
  if (typeof chrome === 'undefined' || !chrome.developerPrivate) {
    throw new Error('developerPrivate недоступен');
  }
  await wrap((cb) => chrome.developerPrivate.updateProfileConfiguration({inDeveloperMode: true}, cb));
  return await wrap((cb) => chrome.developerPrivate.loadUnpacked(path, cb));
})()`
	raw, err = pc.callOn(attached.SessionID, "Runtime.evaluate", map[string]any{
		"expression":    expr,
		"awaitPromise":  true,
		"returnByValue": true,
	}, 20*time.Second)
	if err != nil {
		return "", err
	}
	var eval struct {
		Result struct {
			Value json.RawMessage `json:"value"`
			Type  string          `json:"type"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if json.Unmarshal(raw, &eval) != nil {
		return "", fmt.Errorf("Runtime.evaluate: плохой ответ")
	}
	if eval.ExceptionDetails != nil && eval.ExceptionDetails.Text != "" {
		return "", fmt.Errorf("chrome://extensions: %s", eval.ExceptionDetails.Text)
	}
	var id string
	if json.Unmarshal(eval.Result.Value, &id) != nil || id == "" {
		var obj struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(eval.Result.Value, &obj) != nil || obj.ID == "" {
			return "", fmt.Errorf("chrome://extensions: нет id расширения")
		}
		id = obj.ID
	}
	return id, nil
}

func (pc *pipeChrome) loadUnpacked(extDir string, timeout time.Duration) (string, error) {
	time.Sleep(500 * time.Millisecond)
	raw, err := pc.call("Extensions.loadUnpacked", map[string]string{"path": extDir}, timeout)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already") {
			return "", nil
		}
		return "", err
	}
	var res struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &res) != nil || res.ID == "" {
		return "", fmt.Errorf("Extensions.loadUnpacked: нет id расширения")
	}
	return res.ID, nil
}

func (pc *pipeChrome) call(method string, params any, timeout time.Duration) (json.RawMessage, error) {
	return pc.callOn("", method, params, timeout)
}

func (pc *pipeChrome) callOn(sessionID, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	pc.mu.Lock()
	pc.nextID++
	id := pc.nextID
	ch := make(chan cdpMsg, 1)
	pc.pending[id] = ch
	msg := map[string]any{"id": id, "method": method, "params": params}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	body, err := json.Marshal(msg)
	pc.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := writeCDP(pc.to, body); err != nil {
		return nil, fmt.Errorf("cdp write: %w", err)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case msg := <-ch:
		if msg.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, msg.Error.Message)
		}
		return msg.Result, nil
	case <-timer.C:
		return nil, fmt.Errorf("%s: нет ответа", method)
	case <-pc.closed:
		return nil, fmt.Errorf("%s: chrome закрыт", method)
	}
}

func (pc *pipeChrome) readLoop() {
	r := bufio.NewReader(pc.from)
	for {
		select {
		case <-pc.closed:
			return
		default:
		}
		body, err := readCDP(r)
		if err != nil {
			pc.close()
			return
		}
		var msg cdpMsg
		if json.Unmarshal(body, &msg) != nil || msg.ID == 0 {
			continue
		}
		pc.mu.Lock()
		ch := pc.pending[msg.ID]
		delete(pc.pending, msg.ID)
		pc.mu.Unlock()
		if ch != nil {
			select {
			case ch <- msg:
			default:
			}
		}
	}
}

func inheritableDup(h windows.Handle) (windows.Handle, error) {
	var dup windows.Handle
	err := windows.DuplicateHandle(windows.CurrentProcess(), h, windows.CurrentProcess(), &dup, 0, true, windows.DUPLICATE_SAME_ACCESS)
	return dup, err
}

func closePipes(files ...*os.File) {
	for _, f := range files {
		if f != nil {
			_ = f.Close()
		}
	}
}

func writeCDP(w io.Writer, body []byte) error {
	_, err := w.Write(append(append([]byte{}, body...), 0))
	return err
}

func readCDP(r *bufio.Reader) ([]byte, error) {
	body, err := r.ReadBytes(0)
	if err != nil {
		return nil, err
	}
	if len(body) > 0 && body[len(body)-1] == 0 {
		body = body[:len(body)-1]
	}
	return body, nil
}
