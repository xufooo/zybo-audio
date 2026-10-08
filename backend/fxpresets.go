// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"log"
)

type fxPreset struct {
	Name string `json:"name"`
	Cn   string `json:"cn"`
	FX   string `json:"fx"`

	Model int `json:"model,omitempty"`

	Harmonics [10]float64 `json:"harmonics"`

	WetGain float64 `json:"wet_gain"`

	HPFHz float64 `json:"hpf_hz"`
	LPFHz float64 `json:"lpf_hz"`

	Gear        float64 `json:"gear,omitempty"`
	Reconstruct int     `json:"reconstruct,omitempty"`

	Source string `json:"source"`
}

var fxPresetNames = [4]string{"analogx-slight", "analogx-moderate", "analogx-extreme", "vse"}

var fxAnalogXPresetIDSuffix = [3]string{"slight", "moderate", "extreme"}

var fxAnalogXCn = [3]string{"Slight", "Moderate", "Extreme"}

func fxPresetTable() []fxPreset {
	out := make([]fxPreset, 0, len(fxPresetNames))
	for i, t := range analogxTiers {
		out = append(out, fxPreset{
			Name:      "analogx-" + fxAnalogXPresetIDSuffix[i],
			Cn:        fxAnalogXCn[i],
			FX:        "analogx",
			Model:     i,
			Harmonics: analogxHarmonics,
			WetGain:   t.Gain,
			HPFHz:     240.0,
			LPFHz:     t.LPHz,
			Source: "refs/viperfx-re/src/viper/effects/AnalogX.cpp:5-16 (harmonic table)," +
				":62-93 (three tiers of gain/LPF),:53-57 (HP240/Peak633); " +
				"tier names refs/viper4android_fx/android_4.x/res/values/arrays.xml:266-274",
		})
	}

	gear := vseGearMin
	p, err := vseExciterParams(gear)
	if err != nil {
		log.Printf("fxpresets: cannot decode VSE default tier %g (%v), falling back to factory shape", gear, err)
		p = exciterDefaultParams()
	}
	rec, err := vseReconstruct(gear)
	if err != nil {
		log.Printf("fxpresets: cannot decode reconstruct for VSE default tier %g (%v), deriving from mix*100 instead", gear, err)
		rec = int(p.Mix*100 + 0.5)
	}
	out = append(out, fxPreset{
		Name:        "vse",
		Cn:          "VSE treble exciter (default tier)",
		FX:          "vse",
		Harmonics:   p.Harmonics,
		WetGain:     p.Mix,
		HPFHz:       p.HPFHz,
		LPFHz:       p.LPFHz,
		Gear:        gear,
		Reconstruct: rec,
		Source: "refs/viperfx-re/src/viper/effects/SpectrumExtend.cpp:4-15 (odd harmonics at 0.02)," +
			":19 (refFreq 7600),:44-52 (HP=refFreq / LP=fs/2-2000); " +
			"tier-to-core mapping refs/viper4android_fx/android_4.x-5.x/src/com/vipercn/" +
			"viper4android_v2/service/ViPER4AndroidService.java:1708-1710; " +
			"default tier 0.1 refs/viper4android_fx/android_4.x/res/xml/headset_preferences_l2.xml:85-92",
	})
	return out
}

func fxPresetByName(name string) (fxPreset, error) {
	for _, p := range fxPresetTable() {
		if p.Name == name {
			return p, nil
		}
	}
	return fxPreset{}, fmt.Errorf("No such parameter preset %q; this machine has four groups: %v"+
		"(AnalogX tiers: AnalogX.cpp:62-93; VSE: SpectrumExtend.cpp:4-15)",
		name, fxPresetNames[:])
}

func applyFXPreset(name string) error {
	p, err := fxPresetByName(name)
	if err != nil {
		return err
	}
	switch p.FX {
	case "analogx":
		params := analogxParams{Model: p.Model}
		return setAnalogX(&params)
	case "vse":
		gear := p.Gear
		return setVSE(&gear)
	}
	return fmt.Errorf("Parameter preset %q has unknown effect type %q (locally only analogx / vse)", name, p.FX)
}

func fxPresetCurrent() []string {
	out := make([]string, 0, 2)
	if currentAnalogX != nil {
		m := currentAnalogX.Model
		if m >= 0 && m < len(fxAnalogXPresetIDSuffix) {
			out = append(out, "analogx-"+fxAnalogXPresetIDSuffix[m])
		}
	}

	if currentExciter != nil {
		if gear, ok := vseGearOf(*currentExciter); ok && gear == vseGearMin {
			out = append(out, "vse")
		}
	}
	return out
}

func fxPresetsView() map[string]any {
	on := fxPresetCurrent()
	return map[string]any{
		"presets": fxPresetTable(),
		"on":      on,
		"note": "A2' four parameter presets (three AnalogX tiers + VSE default tier)." +
			"Deploy in one line: PUT /api/dsp/chain {\"fx_preset\":\"analogx-moderate\"}." +
			"They land on the existing analogx / exciter fields (persistence convention unchanged)." +
			"Disabling still uses each one's off field (analogx.off / vse.off).",
	}
}
