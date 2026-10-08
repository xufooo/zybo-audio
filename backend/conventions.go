// SPDX-License-Identifier: GPL-2.0-only
package main

import "math"

func gainClassDB(v float64) float64 {
	return 20 * math.Log10(1+v/100.0)
}

func levelClassDB(v float64) float64 {
	if v <= 0 {
		return math.Inf(-1)
	}
	return 20 * math.Log10(v/100.0)
}

func gainClassLinear(v float64) float64 { return 1 + v/100.0 }

func levelClassLinear(v float64) float64 { return v / 100.0 }

type dbClass string

const (
	dbClassLevel dbClass = "level"

	dbClassGain dbClass = "gain"

	dbClassLinear dbClass = "linearByConstruction"

	dbClassNA dbClass = "notApplicable"
)

type panelDBConvention struct {
	Effect  string
	Param   string
	Panel   float64
	Class   dbClass
	WantDB  float64
	Wet     float64
	Caller  string
	Source  string
	Verdict string
}

var panelDBConventions = []panelDBConvention{
	{
		Effect: "output_volume", Param: "0x10032 PARAM_HPFX_OUTPUT_VOLUME (panel key viper4android.headphonefx.outvol)",
		Panel: 50, Class: dbClassLevel, WantDB: levelClassDB(50),
		Caller: "Not implemented (local volume uses the ALSA codec's own dB grid, see pctToDB in volume.go)",
		Source: "refs/viperfx-re/src/viper/ViPER.cpp:414-416 (frameScale = val1/100); " +
			"Panel and defaults refs/viper4android_fx/android_4.x/res/xml/headset_preferences_l2.xml:403-410; " +
			"issue path ViPER4AndroidService.java:1905-1906; dB labels are arrays.xml's output_db",
		Verdict: "No change. V4A's integer panel scale 0-100 does not exist locally (local volume is its own 1-100 slider + " +
			"the codec's 1 dB grid, calibrated in volume.go against measured dB). Registered here to keep " +
			"the conversion basis on record, not to adopt V4A's slider",
	},
	{
		Effect: "limiter_threshold", Param: "0x10034 PARAM_HPFX_LIMITER_THRESHOLD (panel key viper4android.headphonefx.limiter)",
		Panel: 50, Class: dbClassLevel, WantDB: levelClassDB(50),
		Caller: "Not implemented (local /api/dsp/limiter takes dB directly: dspLimiterThrDB, see dsp.go)",
		Source: "Panel entries = arrays.xml's output_db (labels are dB), default 100 " +
			"refs/viper4android_fx/android_4.x/res/xml/headset_preferences_l2.xml:421-428; " +
			"issue path ViPER4AndroidService.java:1909-1910",
		Verdict: "No change. The local interface unit is already dB (-30-0), with no percentage step; " +
			"if the panel ever grows V4A-style 0-100 tiers, they must go through levelClassDB (output_db labels " +
			"100<->0 / 90<->-0.92 / 50<->-6.02 verified point by point)",
	},
	{
		Effect: "viperbass_natural", Param: "0x10029 PARAM_HPFX_VIPERBASS_BASSGAIN (vbass_boost 50-600)",
		Panel: 50, Class: dbClassGain, WantDB: gainClassDB(50),
		Caller: "viperbass.go:196 / :266 (wet := p.Gain/100, one site per branch)",
		Source: "refs/viperfx-re/src/viper/ViPER.cpp:351 (SetBassFactor(val1/100)); " +
			"refs/viperfx-re/src/viper/effects/ViPERBass.cpp:42-43 (out = x + LP(x)*bassFactor)",
		Verdict: "No change (already gain class). Signal flow is `x + (g/100)*LP(x)`; mono low end gives " +
			"1 + g/100 = 1.5 = +3.52 dB, bit-identical to gainClassDB(50)." +
			"The implementation writes the wet coefficient g/100 (not 1+g/100), which is correct: writing gainClassLinear(50) " +
			"would also scale the dry signal (3.5 dB too much)",
	},
	{
		Effect: "clarity_ozone", Param: "0x1002C PARAM_HPFX_VIPERCLARITY_CLARITY (panel 0-100)",
		Panel: 50, Class: dbClassGain, WantDB: gainClassDB(50),
		Caller: "clarity.go:96 (gainClassDB inside highShelfOzone)",
		Source: "refs/viperfx-re/src/viper/effects/ViPERClarity.cpp:63-67" +
			"(SetGain(clarityGainPercent + 1.0)); " +
			"refs/viperfx-re/src/viper/utils/HighShelf.cpp:17-19 (gain_dB = 20*log10(gain)); " +
			"dimensions per ViPER.cpp:362-364 (SetClarity(val1/100))",
		Verdict: "No change (already gain class, numerically bit-identical to the source). The source convention is " +
			"20*log10(g+1) with g = Level/100, which is exactly gainClassDB(Level)." +
			"The implementation takes a detour through `gainClassDB((gainLinear-1)*100)` (converting gainLinear back to a panel value before applying the formula)," +
			"mathematically equivalent but hard to read -- rewritten to state the source line directly as 20*log10(gainLinear)," +
			"with a test pinning both equal",
	},
	{
		Effect: "clarity_natural", Param: "NoiseSharpening pre-emphasis coefficient (same 0x1002C panel value)",
		Panel: 50, Class: dbClassLinear, WantDB: 20 * math.Log10(0.5), Wet: 0.5,
		Caller: "clarity.go:120-127 (g = Level/100 in noiseSharpeningBiquad)",
		Source: "refs/viperfx-re/src/viper/effects/ViPERClarity.cpp:64 (SetGain(clarityGainPercent)); " +
			"refs/viperfx-re/src/viper/utils/NoiseSharpening.cpp:45-47 (this->gain = gain, linear); " +
			":18-19 (x + g*(x - x_1))",
		Verdict: "No change. Here g is a plain linear coefficient (NoiseSharpening::SetGain applies no dB mapping);" +
			"panel value 50 gives g=0.5, so pre-emphasis peaks at 20log10(1+2g)=+6.02 dB (at z=-1)." +
			"Neither dB formula applies here -- forcing the gain class would give +3.52 dB (off by 2.5 dB)",
	},
	{
		Effect: "vse_exciter", Param: "0x1000E PARAM_SPECTRUM_EXTENSION_BARK_RECONSTRUCT (panel tiers 0.1-1.0)",
		Panel: 0.1, Class: dbClassLinear, WantDB: 20 * math.Log10(0.56), Wet: 0.56,
		Caller: "vse.go:95-103 (vseExciterParams => Mix = reconstruct/100); harmonic.go:118 (factory 0.56)",
		Source: "refs/viper4android_fx/android_4.x-5.x/src/com/vipercn/viper4android_v2/service/" +
			"ViPER4AndroidService.java:1708-1710 (nSEValue = round(UI*5.6*100)); " +
			"refs/viperfx-re/src/viper/ViPER.cpp:242-245 (SetExciter(val1/100)); " +
			"refs/viperfx-re/src/viper/effects/SpectrumExtend.cpp:33,38 (tmp * this->exciter)",
		Verdict: "No change. The core simply multiplies linearly: panel 0.1 => integer 56 => in-core 0.56 (-5.03 dB wet)." +
			"Neither dB formula applies (the gain class would give +3.52 dB, off by 8.5 dB)." +
			"WARNING: panel full scale 1.0 => 5.6 exceeds the local MIX2 Q3.15 coefficient ceiling of 4.0, so this machine relies on" +
			"whole-polynomial scaling + one gain stage to fold it into range (harmonic.go:56-89)," +
			"failing loudly when it does not fit (validateExciterMix) instead of silently truncating",
	},
	{
		Effect: "analogx_model", Param: "0x10031 PARAM_ANALOGX_MODE (tier index 0/1/2)",
		Panel: 0, Class: dbClassNA, WantDB: 20 * math.Log10(0.6), Wet: 0.6,
		Caller: "analogx.go:38-45 (analogxTiers)",
		Source: "refs/viperfx-re/src/viper/ViPER.cpp:410-412 (SetProcessingModel(val1), index only); " +
			"refs/viperfx-re/src/viper/effects/AnalogX.cpp:62-93 (gains 0.6/1.2/2.4 are fixed per tier)",
		Verdict: "No change. This panel item sends only the tier index; no percentage passes through;" +
			"gains 0.6/1.2/2.4 are in-core per-tier constants, so neither dB formula applies." +
			"(The Wet column holds model 0's 0.6 only as a reference value for WantDB)",
	},
	{
		Effect: "agc_playback_gain", Param: "0x1001F PARAM_HPFX_AGC_VOLUME (AGC linear ceiling)",
		Panel: 50, Class: dbClassLevel, WantDB: levelClassDB(50),
		Caller: "Not implemented (upstream V4A's `PlaybackGain::Process` write-back loop body is empty)",
		Source: "refs/viperfx-re/src/viper/utils/../effects/PlaybackGain.cpp:74-77" +
			"(`for (...) { }` empty body); dimensions: see header of this file :12",
		Verdict: "Not planned (decided). Upstream is an empty shell, so there is no algorithm to port;" +
			"registered here only to record the conclusion that it is level class, so it is not re-debated later",
	},
}

var panelDBLog10Files = map[string]string{
	"conventions.go":  "Definitions of the two formulas themselves (gainClassDB / levelClassDB)",
	"clarity.go":      "OZONE gain-class conversion (registered as clarity_ozone) + NATURAL echo ceiling 20log10(1+2g)",
	"crossfeed.go":    "20log10(Ghi) in bs2b's Fchi formula -- not a panel-value conversion, part of bs2b's own definition",
	"dsp.go":          "Limiter threshold default (back-computed from the Q1.15 constant) and preamp (back-computed from linear gain) -- both internal quantities",
	"dsp_headroom.go": "Magnitude of complex frequency response (headroom computation), not panel values",
	"dsp_slot.go":     "IR gain report (Q3.15 -> dB), not panel values",
	"response.go":     "Frequency response curve (complex magnitude -> dB), not panel values",
	"vdc.go":          "DDC frequency response (complex magnitude -> dB), not panel values",
}
