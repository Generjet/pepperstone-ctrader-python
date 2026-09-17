package fxbot

import "math"

func sma(values []float64, period int) []float64 {
	r := make([]float64, len(values))
	for i := range r {
		r[i] = math.NaN()
	}
	if period <= 0 || len(values) < period {
		return r
	}
	first := -1
	for i, v := range values {
		if !math.IsNaN(v) {
			first = i
			break
		}
	}
	if first < 0 || first+period > len(values) {
		return r
	}
	sum := 0.0
	for i := first; i < first+period; i++ {
		sum += values[i]
	}
	r[first+period-1] = sum / float64(period)
	for i := first + period; i < len(values); i++ {
		sum = sum - values[i-period] + values[i]
		r[i] = sum / float64(period)
	}
	return r
}

func wma(values []float64, period int) []float64 {
	r := make([]float64, len(values))
	for i := range r {
		r[i] = math.NaN()
	}
	if period <= 0 || len(values) < period {
		return r
	}
	weight := 0.0
	for i := 1; i <= period; i++ {
		weight += float64(i)
	}
	for i := period - 1; i < len(values); i++ {
		sum := 0.0
		for j := 0; j < period; j++ {
			sum += values[i-j] * float64(period-j)
		}
		r[i] = sum / weight
	}
	return r
}

// hma computes the Hull Moving Average of the given series.
func hma(values []float64, period int) []float64 {
	r := make([]float64, len(values))
	for i := range r {
		r[i] = math.NaN()
	}
	if period <= 0 {
		return r
	}
	half := period / 2
	if half < 1 {
		half = 1
	}
	sqrt := int(math.Sqrt(float64(period)))
	if sqrt < 1 {
		sqrt = 1
	}
	w1 := wma(values, half)
	w2 := wma(values, period)
	diff := make([]float64, len(values))
	for i := range values {
		if !math.IsNaN(w1[i]) && !math.IsNaN(w2[i]) {
			diff[i] = 2*w1[i] - w2[i]
		}
	}
	return wma(diff, sqrt)
}

func calculateRSI(closes []float64, period int) []float64 {
	r := make([]float64, len(closes))
	for i := range r {
		r[i] = math.NaN()
	}
	if len(closes) < period+1 {
		return r
	}

	gainSum, lossSum := 0.0, 0.0
	for i := 1; i <= period; i++ {
		diff := closes[i] - closes[i-1]
		if diff > 0 {
			gainSum += diff
		} else {
			lossSum -= diff
		}
	}

	avgGain := gainSum / float64(period)
	avgLoss := lossSum / float64(period)
	if avgLoss == 0 {
		r[period] = 100
	} else {
		r[period] = 100 - 100/(1+avgGain/avgLoss)
	}

	for i := period + 1; i < len(closes); i++ {
		diff := closes[i] - closes[i-1]
		gain, loss := 0.0, 0.0
		if diff > 0 {
			gain = diff
		} else {
			loss = -diff
		}
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
		if avgLoss == 0 {
			r[i] = 100
		} else {
			r[i] = 100 - 100/(1+avgGain/avgLoss)
		}
	}
	return r
}

func calculateStochastic(highs, lows, closes []float64, kPeriod, kSmooth, dPeriod int) ([]float64, []float64) {
	n := len(closes)
	rawK := make([]float64, n)
	for i := range rawK {
		rawK[i] = math.NaN()
	}

	for i := kPeriod - 1; i < n; i++ {
		hh := highs[i-kPeriod+1]
		ll := lows[i-kPeriod+1]
		for j := i - kPeriod + 2; j <= i; j++ {
			if highs[j] > hh {
				hh = highs[j]
			}
			if lows[j] < ll {
				ll = lows[j]
			}
		}
		if hh == ll {
			rawK[i] = 50
		} else {
			rawK[i] = 100 * (closes[i] - ll) / (hh - ll)
		}
	}

	smoothedK := sma(rawK, kSmooth)
	stochD := sma(smoothedK, dPeriod)

	return smoothedK, stochD
}
