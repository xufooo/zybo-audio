// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"math"
)

type crossfeedParams struct {
	FcutHz float64 `json:"fcut_hz"`
	Feed   float64 `json:"feed"`

	PassFilter bool `json:"pass_filter,omitempty"`
}

var crossfeedTiers = []struct {
	Name   string  `json:"name"`
	Cn     string  `json:"cn"`
	FcutHz float64 `json:"fcut_hz"`
	Feed   float64 `json:"feed"`
}{
	{"Slight", "Slight", 650, 95},
	{"Moderate", "Moderate", 700, 60},
	{"Extreme", "Extreme", 700, 45},
}

const crossfeedTierIndexModerate = 1

func crossfeedTierOf(p crossfeedParams) int {
	for i, t := range crossfeedTiers {
		if t.FcutHz == p.FcutHz && t.Feed == p.Feed {
			return i
		}
	}
	return -1
}

func crossfeedDefaultParams() crossfeedParams {
	t := crossfeedTiers[crossfeedTierIndexModerate]
	return crossfeedParams{FcutHz: t.FcutHz, Feed: t.Feed}
}

const (
	bs2bMinFcut = 300.0
	bs2bMaxFcut = 2000.0
	bs2bMinFeed = 10.0
	bs2bMaxFeed = 150.0
)

func crossfeedCoefs(p crossfeedParams) (lo [5]int32, hi [5]int32, mix [2]int32, err error) {
	fcut := p.FcutHz
	if fcut < bs2bMinFcut {
		fcut = bs2bMinFcut
	}
	if fcut > bs2bMaxFcut {
		fcut = bs2bMaxFcut
	}
	feed := p.Feed
	if feed < bs2bMinFeed {
		feed = bs2bMinFeed
	}
	if feed > bs2bMaxFeed {
		feed = bs2bMaxFeed
	}

	level := feed / 10.0

	GBlo := level*-5.0/6.0 - 3.0
	GBhi := level/6.0 - 3.0

	Glo := math.Pow(10, GBlo/20.0)
	Ghi := 1.0 - math.Pow(10, GBhi/20.0)

	Fchi := fcut * math.Pow(2.0, (GBlo-20.0*math.Log10(Ghi))/12.0)

	xLo := math.Exp(-2.0 * math.Pi * fcut / sampleRate)
	a0Lo := Glo * (1.0 - xLo)

	xHi := math.Exp(-2.0 * math.Pi * Fchi / sampleRate)
	a0Hi := 1.0 - Ghi*(1.0-xHi)
	a1Hi := -xHi

	gain := 1.0 / (1.0 - Ghi + Glo)

	lo = [5]int32{floatToQ315(a0Lo), 0, 0, floatToQ315(-xLo), 0}

	hi = [5]int32{floatToQ315(a0Hi), floatToQ315(a1Hi), 0, floatToQ315(-xHi), 0}
	mix = [2]int32{floatToQ315(gain), floatToQ315(gain)}

	for _, c := range []struct {
		name string
		v    int32
	}{
		{"lo.b0", lo[0]}, {"lo.a1", lo[3]},
		{"hi.b0", hi[0]}, {"hi.b1", hi[1]}, {"hi.a1", hi[3]},
		{"mix", mix[0]},
	} {
		if _, err := checkCoef(c.name, c.v); err != nil {
			return lo, hi, mix, err
		}
	}
	return lo, hi, mix, nil
}

func crossfeedEffectiveFcut(p crossfeedParams) float64 {
	f := p.FcutHz
	if f < bs2bMinFcut {
		f = bs2bMinFcut
	}
	if f > bs2bMaxFcut {
		f = bs2bMaxFcut
	}
	return f
}

var currentCrossfeed *crossfeedParams

func crossfeedAvailable() bool {
	return dspEngineGen == 1 && dspReadEngineCaps().Mix2Available()
}

func setCrossfeed(p *crossfeedParams) error {
	if p == nil {
		currentCrossfeed = nil
		markStateDirty()
		return nil
	}
	if !crossfeedAvailable() {
		return fmt.Errorf("This hardware has no MIX2 slot (CAP1 bit3 not set); crossfeed cannot be deployed")
	}

	if _, _, _, err := crossfeedCoefs(*p); err != nil {
		return err
	}
	q := *p
	currentCrossfeed = &q
	markStateDirty()
	return nil
}

func crossfeedView() any {
	if currentCrossfeed == nil {
		return nil
	}
	p := *currentCrossfeed
	return map[string]any{
		"fcut_hz": p.FcutHz, "feed": p.Feed,
		"tier":              crossfeedTierOf(p),
		"effective_fcut_hz": crossfeedEffectiveFcut(p),
		"available":         crossfeedAvailable(),
		"note":              "bs2b replica: lower fcut crosses over less low end, larger feed crosses over more gently (unit 0.1 dB). Mono content passes through it with unchanged level",
	}
}

func crossfeedTiersView() map[string]any {
	out := make([]map[string]any, 0, len(crossfeedTiers))
	for i, t := range crossfeedTiers {
		out = append(out, map[string]any{
			"tier":    i,
			"name_en": t.Name,
			"name_cn": t.Cn,
			"fcut_hz": t.FcutHz,
			"feed":    t.Feed,
			"default": i == crossfeedTierIndexModerate,
		})
	}
	return map[string]any{
		"tiers":        out,
		"default_tier": crossfeedTierIndexModerate,
		"v4a_default":  0,
		"source":       "refs/viperfx-re/src/viper/ViPER.cpp:370-401 (three-tier values)",
		"source_names": "refs/viper4android_fx/android_4.x/res/values/arrays.xml:256-265 + values-zh-rCN/arrays.xml:98-102",
		"crosscheck":   "refs/jamesdsp-linux/resources/assets/default.conf:16-17 (700/60 verified bit for bit)",
		"default_why":  "Local default follows the owner's JamesDSP installed config (700/60 = V4A Moderate), not V4A tier 0",
	}
}
