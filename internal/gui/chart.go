package gui

// point — пиксель графика.
type point struct{ X, Y int }

// sparkline раскладывает цены по прямоугольнику. Пустой срез или нулевая
// область дают nil — вызывающий рисует заглушку.
func sparkline(width, height int, prices []int64) []point {
	if width < 2 || height < 2 || len(prices) == 0 {
		return nil
	}
	min, max := prices[0], prices[0]
	for _, p := range prices[1:] {
		if p < min {
			min = p
		}
		if p > max {
			max = p
		}
	}
	span := max - min
	if span == 0 {
		span = 1
	}
	top, bottom := 3, height-4
	if bottom <= top {
		top, bottom = 0, height-1
	}
	usable := bottom - top
	out := make([]point, len(prices))
	if len(prices) == 1 {
		out[0] = point{X: width / 2, Y: top + usable/2}
		return out
	}
	dx := float64(width-1) / float64(len(prices)-1)
	for i, p := range prices {
		y := bottom - int(float64(p-min)/float64(span)*float64(usable))
		out[i] = point{X: int(float64(i) * dx), Y: y}
	}
	return out
}
