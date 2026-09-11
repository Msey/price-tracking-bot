package gui

// point — пиксель графика.
type point struct{ X, Y int }

// sparkline раскладывает цены по прямоугольнику. Узлы стоят по центру
// равных долей ширины: три точки — на каждой трети, десять — на каждой десятой.
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
	for i, p := range prices {
		y := bottom - int(float64(p-min)/float64(span)*float64(usable))
		out[i] = point{X: nodeX(width, len(prices), i), Y: y}
	}
	return out
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
