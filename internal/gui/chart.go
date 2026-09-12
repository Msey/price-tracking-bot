package gui

import "math"

// point — пиксель графика.
type point struct{ X, Y int }

// sparkline раскладывает цены по прямоугольнику. Узлы стоят по центру
// равных долей ширины: три точки — на каждой трети, десять — на каждой десятой.
func sparkline(width, height int, prices []int64) []point {
	return sparklineInto(nil, width, height, prices)
}

// sparklineInto пишет узлы в dst, если ёмкости хватает. Иначе выделяет
// новый срез. Пустой вход возвращает dst[:0], чтобы не терять буфер.
func sparklineInto(dst []point, width, height int, prices []int64) []point {
	if width < 2 || height < 2 || len(prices) == 0 {
		if dst == nil {
			return nil
		}
		return dst[:0]
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
	// Отступы сверху и снизу — доля высоты, а не фиксированные пиксели:
	// на экране с двойной плотностью график иначе упирался бы в края строки.
	inset := height / 14
	if inset < 1 {
		inset = 1
	}
	top, bottom := inset, height-1-inset
	if bottom <= top {
		top, bottom = 0, height-1
	}
	usable := bottom - top
	if cap(dst) < len(prices) {
		dst = make([]point, len(prices))
	} else {
		dst = dst[:len(prices)]
	}
	for i, p := range prices {
		y := bottom - int(float64(p-min)/float64(span)*float64(usable))
		dst[i] = point{X: nodeX(width, len(prices), i), Y: y}
	}
	return dst
}

// singlePriceSpan — горизонталь через единственный узел, на всю ширину
// графика. Второго замера нет, ломаную не из чего собрать, но линия
// всё равно показывает уровень цены.
func singlePriceSpan(width int, pt point) (left, right point) {
	if width < 1 {
		return pt, pt
	}
	return point{X: 0, Y: pt.Y}, point{X: width, Y: pt.Y}
}

// nodeHalfWidth — полуширина ряда узла. У маленького GDI-эллипса сверху
// торчит один пиксель; крайние ряды делаем шире, чтобы силуэт был ровный.
func nodeHalfWidth(r, dy int) int {
	if r < 1 {
		return 0
	}
	ad := dy
	if ad < 0 {
		ad = -ad
	}
	if ad > r {
		return 0
	}
	if ad == r && r > 1 {
		return r - 1
	}
	return r
}

// nodeX — центр i-й доли контрола шириной width при n узлах.
func nodeX(width, n, i int) int {
	if n < 1 || width < 1 {
		return 0
	}
	if i < 0 {
		i = 0
	}
	if i >= n {
		i = n - 1
	}
	return (i*2 + 1) * width / (2 * n)
}

// hitSample возвращает индекс доли под координатой x.
func hitSample(width, n, x int) int {
	if n < 1 || width < 1 {
		return -1
	}
	if x < 0 {
		x = 0
	}
	if x >= width {
		x = width - 1
	}
	i := x * n / width
	if i >= n {
		return n - 1
	}
	return i
}

// sparkHandles — контрольные точки кубики с горизонтальными касательными:
// кривая выходит с уровня from и заходит в to без излома на узле.
func sparkHandles(from, to point) (c1, c2 point) {
	dx := to.X - from.X
	return point{X: from.X + dx/3, Y: from.Y}, point{X: to.X - dx/3, Y: to.Y}
}

func cubicBezier(p0, c1, c2, p1 point, t float64) point {
	if t <= 0 {
		return p0
	}
	if t >= 1 {
		return p1
	}
	u := 1 - t
	uu, tt := u*u, t*t
	x := uu*u*float64(p0.X) + 3*uu*t*float64(c1.X) + 3*u*tt*float64(c2.X) + tt*t*float64(p1.X)
	y := uu*u*float64(p0.Y) + 3*uu*t*float64(c1.Y) + 3*u*tt*float64(c2.Y) + tt*t*float64(p1.Y)
	return point{X: int(math.Round(x)), Y: int(math.Round(y))}
}

func bezierSteps(from, to point) int {
	if from == to {
		return 0
	}
	dx := to.X - from.X
	if dx < 0 {
		dx = -dx
	}
	dy := to.Y - from.Y
	if dy < 0 {
		dy = -dy
	}
	d := dx
	if dy > d {
		d = dy
	}
	n := d / 6
	if n < 8 {
		return 8
	}
	if n > 32 {
		return 32
	}
	return n
}

// appendCubic дописывает кубику Безье от from к to. Первая точка
// не дублируется, если она уже последний элемент dst.
func appendCubic(dst []point, from, to point) []point {
	if len(dst) == 0 || dst[len(dst)-1] != from {
		dst = append(dst, from)
	}
	n := bezierSteps(from, to)
	if n < 1 {
		return dst
	}
	c1, c2 := sparkHandles(from, to)
	for i := 1; i <= n; i++ {
		dst = append(dst, cubicBezier(from, c1, c2, to, float64(i)/float64(n)))
	}
	return dst
}

// priceMove — направление от одного замера к следующему:
// −1 падение, 0 без изменения, +1 рост.
func priceMove(from, to int64) int {
	switch {
	case to < from:
		return -1
	case to > from:
		return 1
	default:
		return 0
	}
}

// sampleChartLabel — ценник на смене цены или на первом «нет в наличии»
// после живого замера, чтобы серая точка не оставалась без числа.
func sampleChartLabel(samples []Sample, i int) bool {
	if i < 0 || i >= len(samples) {
		return false
	}
	if !samples[i].Available {
		return i == 0 || samples[i-1].Available
	}
	if i > 0 && !samples[i-1].Available {
		return true
	}
	if i == 0 {
		return true
	}
	return samples[i].Price != samples[i-1].Price
}

// firstPriceLabel — писать цену только на первом узле группы с одной
// ценой. Пока цена не сменилась, остальные точки остаются без подписи.
func firstPriceLabel(prices []int64, i int) bool {
	if i < 0 || i >= len(prices) {
		return false
	}
	if i == 0 {
		return true
	}
	return prices[i] != prices[i-1]
}

// trashLayout — график слева, урна справа: между ними зазор, у правого
// края строки поле pad.
func trashLayout(rowWidth, pad, gap, trash int) (chartWidth, trashX int) {
	if trash < 0 {
		trash = 0
	}
	trashX = rowWidth - pad - trash
	chartWidth = trashX - pad - gap
	if chartWidth < 0 {
		chartWidth = 0
	}
	return
}
