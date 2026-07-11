package domain

import "math"

// VPD returns the air vapour-pressure deficit in kPa for the given air
// temperature (°C) and relative humidity (%).
//
// It uses the Tetens saturation-vapour-pressure approximation:
//
//	SVP(T) = 0.61078 · exp(17.27·T / (T + 237.3))   [kPa]
//	VPD    = SVP · (1 − RH/100)
//
// This is air VPD; leaf VPD (using a leaf temperature a couple of degrees below
// air) can be layered on later.
func VPD(tempC, humidity float64) float64 {
	return LeafVPD(tempC, humidity, 0)
}

// LeafVPD returns the vapour-pressure deficit between a leaf and the air.
// leafTempOffsetC is added to airTempC to estimate leaf temperature. An offset
// of zero is ordinary air VPD; a typical estimate for an illuminated leaf is -2.
func LeafVPD(airTempC, humidity, leafTempOffsetC float64) float64 {
	svp := func(tempC float64) float64 {
		return 0.61078 * math.Exp(17.27*tempC/(tempC+237.3))
	}
	vpd := svp(airTempC+leafTempOffsetC) - svp(airTempC)*(humidity/100)
	if vpd < 0 {
		vpd = 0
	}
	return vpd
}
