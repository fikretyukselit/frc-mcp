package retrieve

import "math"

func sigmoid(z float64) float64 { return 1 / (1 + math.Exp(-z)) }
