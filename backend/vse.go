// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"math"
)

const vseReferenceBarkHz = 7600.0

const (
	vseGearMin = 0.1
	vseGearMax = 1.0

	vseGearStep = 0.1

	vseReconstructPerGear = 5.6 * 100.0
)

func vseReconstruct(gear float64) (int, error) {
	if math.IsNaN(gear) || math.IsInf(gear, 0) {
		return 0, fmt.Errorf("VSE gear is not finite number")
	}
	if gear < vseGearMin-1e-9 || gear > vseGearMax+1e-9 {
		return 0, fmt.Errorf("VSE gear %g outside panel range %g...%g(arrays.xml 10 gear)",
			gear, vseGearMin, vseGearMax)
	}

	steps := gear / vseGearStep
	if math.Abs(steps-math.Round(steps)) > 1e-6 {
		return 0, fmt.Errorf("VSE gear %g is not panel gears:panel only has "+
			"0.1/0.2/.../1.0 ten discrete gears(per gear +56)", gear)
	}
	return int(math.Round(gear * vseReconstructPerGear)), nil
}

func vseExciterParams(gear float64) (exciterParams, error) {
	r, err := vseReconstruct(gear)
	if err != nil {
		return exciterParams{}, err
	}
	p := exciterDefaultParams()
	p.Mix = float64(r) / 100.0
	return p, nil
}

func vseGearOf(p exciterParams) (float64, bool) {
	def := exciterDefaultParams()
	f := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
	if len(p.Harmonics) != len(def.Harmonics) {
		return 0, false
	}
	for i := range p.Harmonics {
		if !f(p.Harmonics[i], def.Harmonics[i]) {
			return 0, false
		}
	}
	if !f(p.HPFHz, def.HPFHz) || !f(p.LPFHz, def.LPFHz) {
		return 0, false
	}
	for steps := 1; steps <= 10; steps++ {
		gear := float64(steps) * vseGearStep
		r, err := vseReconstruct(gear)
		if err != nil {
			continue
		}
		if f(p.Mix, float64(r)/100.0) {
			return gear, true
		}
	}
	return 0, false
}

func setVSE(gear *float64) error {
	if gear == nil {
		return setExciter(nil)
	}
	p, err := vseExciterParams(*gear)
	if err != nil {
		return err
	}
	return setExciter(&p)
}

func vseView() any {
	if currentExciter == nil {
		return nil
	}
	gear, ok := vseGearOf(*currentExciter)
	if !ok {
		return nil
	}
	r, _ := vseReconstruct(gear)
	return map[string]any{
		"gear":        gear,
		"reconstruct": r,
		"reference":   vseReferenceBarkHz,
		"mix":         currentExciter.Mix,
		"harmonics":   currentExciter.Harmonics,
	}
}

func vseAvailable() bool { return exciterAvailable() }

func vseGearTable() []map[string]any {
	out := make([]map[string]any, 0, 10)
	for steps := 1; steps <= 10; steps++ {
		gear := float64(steps) * vseGearStep
		r, err := vseReconstruct(gear)
		if err != nil {
			continue
		}
		out = append(out, map[string]any{
			"gear":        gear,
			"reconstruct": r,
			"mix":         float64(r) / 100.0,
		})
	}
	return out
}
