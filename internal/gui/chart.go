package gui

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
