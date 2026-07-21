package eval

func Epsilon(composites []float64) float64 {
	if len(composites) == 0 {
		return 0
	}
	min, max := composites[0], composites[0]
	for _, value := range composites[1:] {
		if value < min {
			min = value
		}
		if value > max {
			max = value
		}
	}
	return max - min
}

func Accept(candidate, best, epsilon float64) bool {
	return candidate > best+epsilon
}
