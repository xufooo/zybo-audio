// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"math/cmplx"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	refsViPERcpp        = "../../../refs/viperfx-re/src/viper/ViPER.cpp"
	refsArraysXML       = "../../../refs/viper4android_fx/android_4.x/res/values/arrays.xml"
	refsArraysZhCN      = "../../../refs/viper4android_fx/android_4.x/res/values-zh-rCN/arrays.xml"
	refsJdspDefaultConf = "../../../refs/jamesdsp-linux/resources/assets/default.conf"
	refsJdspMainWindow  = "../../../refs/jamesdsp-linux/src/MainWindow.cpp"
	refsHeadsetPrefsL2  = "../../../refs/viper4android_fx/android_4.x/res/xml/headset_preferences_l2.xml"
	refsDynamicBassCPP  = "../../../refs/viperfx-re/src/viper/utils/DynamicBass.cpp"
)

func readRefs(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Skipf("Reference source missing (%s): %v -- readable on dev machines; skipping only the read-the-source cross-check layer",
			path, err)
	}
	return string(b)
}

func TestA6CrossfeedTiersReadFromSource(t *testing.T) {
	v4a := readRefs(t, refsViPERcpp)

	re := regexp.MustCompile(`case (\d): \{[\s\S]*?\.cutoff = (\d+),[\s\S]*?\.feedback = (\d+),`)
	m := re.FindAllStringSubmatch(v4a, -1)
	if len(m) != 3 {
		t.Fatalf("Extracted %d tiers from ViPER.cpp (want 3 tiers, :370-401) -- source structure changed, regex must follow", len(m))
	}
	for _, g := range m {
		idx, _ := strconv.Atoi(g[1])
		cut, _ := strconv.Atoi(g[2])
		fb, _ := strconv.Atoi(g[3])
		if idx < 0 || idx > 2 {
			t.Fatalf("Tier index %d out of range", idx)
		}
		got := crossfeedTiers[idx]
		if got.FcutHz != float64(cut) || got.Feed != float64(fb) {
			t.Errorf("ViPER.cpp case %d is (cutoff=%d, feedback=%d); local tier table is (%g, %g)",
				idx, cut, fb, got.FcutHz, got.Feed)
		}
	}

	en := readRefs(t, refsArraysXML)
	if !strings.Contains(en, "<item>Slight</item>") || !strings.Contains(en, "<item>Moderate</item>") ||
		!strings.Contains(en, "<item>Extreme</item>") {
		t.Error("arrays.xml:256-260 cure_crossfeed should be Slight/Moderate/Extreme")
	}
	for i, cn := range []string{"Slight", "Moderate", "Extreme"} {
		if crossfeedTiers[i].Cn != cn {
			t.Errorf("Tier %d display name should be %q, got %q",
				i, cn, crossfeedTiers[i].Cn)
		}
	}

	conf := readRefs(t, refsJdspDefaultConf)
	if !strings.Contains(conf, "crossfeed_bs2b_fcut=700") || !strings.Contains(conf, "crossfeed_bs2b_feed=60") {
		t.Error("JamesDSP default.conf:16-17 should be crossfeed_bs2b_fcut=700 / feed=60")
	}
	mw := readRefs(t, refsJdspMainWindow)
	for _, want := range []string{"setValueA(700)", "setValueA(60)", "setValueA(650)", "setValueA(95)"} {
		if !strings.Contains(mw, want) {
			t.Errorf("JamesDSP MainWindow.cpp:975-982 BS2B presets should contain %s", want)
		}
	}

	prefs := readRefs(t, refsHeadsetPrefsL2)
	if !strings.Contains(prefs, "cure.crossfeed") {
		t.Error("cure.crossfeed entry not found in headset_preferences_l2.xml")
	}
	if !strings.Contains(prefs, `android:defaultValue="0"`) {
		t.Log("Note: no cure defaultValue=\"0\" read from headset_preferences_l2.xml -- " +
			"V4A default tier is 0 (Slight); this machine deliberately uses Moderate")
	}
	if !strings.Contains(prefs, `android:defaultValue="100;5600;40;80;50;50"`) {
		t.Error("headset_preferences_l2.xml:274 dynamicsystem.coeffs default should be " +
			"100;5600;40;80;50;50 -- the DYN branch choice (A6 item 3) hangs on this 100")
	}
}

func TestA6PanelDBConventionRegistry(t *testing.T) {
	want := map[string]dbClass{
		"output_volume":     dbClassLevel,
		"limiter_threshold": dbClassLevel,
		"viperbass_natural": dbClassGain,
		"clarity_ozone":     dbClassGain,
		"clarity_natural":   dbClassLinear,
		"vse_exciter":       dbClassLinear,
		"analogx_model":     dbClassNA,
		"agc_playback_gain": dbClassLevel,
	}
	if len(panelDBConventions) != len(want) {
		t.Fatalf("Registry has %d rows, want %d rows (A6 item 2 must cover every conversion site seen in the backend)",
			len(panelDBConventions), len(want))
	}
	seen := map[string]bool{}
	for _, c := range panelDBConventions {
		if _, ok := want[c.Effect]; !ok {
			t.Errorf("Registry has an unexpected row %q", c.Effect)
			continue
		}
		if seen[c.Effect] {
			t.Errorf("Registry row %q appears twice", c.Effect)
		}
		seen[c.Effect] = true
		if c.Class != want[c.Effect] {
			t.Errorf("%s class should be %s, got %s", c.Effect, want[c.Effect], c.Class)
		}
		if c.Param == "" || c.Caller == "" || c.Source == "" || c.Verdict == "" {
			t.Errorf("%s is missing Param/Caller/Source/Verdict -- every row must trace back to source and conclusion", c.Effect)
		}

		switch c.Class {
		case dbClassLevel:
			if math.Abs(c.WantDB-levelClassDB(c.Panel)) > 1e-9 {
				t.Errorf("%s marked as level class, but WantDB=%g != levelClassDB(%g)=%g",
					c.Effect, c.WantDB, c.Panel, levelClassDB(c.Panel))
			}
			if c.Wet != 0 {
				t.Errorf("%s is level class and must not have Wet", c.Effect)
			}
		case dbClassGain:
			if math.Abs(c.WantDB-gainClassDB(c.Panel)) > 1e-9 {
				t.Errorf("%s marked as gain class, but WantDB=%g != gainClassDB(%g)=%g",
					c.Effect, c.WantDB, c.Panel, gainClassDB(c.Panel))
			}
			if c.Wet != 0 {
				t.Errorf("%s is gain class and must not have Wet", c.Effect)
			}
		case dbClassLinear:
			if c.Wet <= 0 {
				t.Errorf("%s marked as linear multiplier, but Wet=%g is not positive", c.Effect, c.Wet)
			}
			if math.Abs(c.WantDB-20*math.Log10(c.Wet)) > 1e-9 {
				t.Errorf("%s WantDB=%g != 20log10(Wet=%g)=%g",
					c.Effect, c.WantDB, c.Wet, 20*math.Log10(c.Wet))
			}
		case dbClassNA:
			if c.Verdict == "" {
				t.Errorf("%s is marked not applicable and must say why", c.Effect)
			}
		default:
			t.Errorf("%s has unknown class %q", c.Effect, c.Class)
		}

		if c.Class == dbClassGain && math.Abs(c.WantDB-levelClassDB(c.Panel)) < 1 {
			t.Errorf("%s gain-class value almost equals the level-class value (panel value %g) -- class may be mislabeled",
				c.Effect, c.Panel)
		}
	}
	for e := range want {
		if !seen[e] {
			t.Errorf("Registry is missing row %q", e)
		}
	}
}

func TestA6PanelDBConventionSitesMatchImplementation(t *testing.T) {
	for _, c := range panelDBConventions {
		switch c.Effect {
		case "clarity_ozone":

			g := c.Panel / 100.0
			if got := 20 * math.Log10(g+1); math.Abs(got-c.WantDB) > 1e-9 {
				t.Errorf("clarity_ozone implementation value %g != registry value %g", got, c.WantDB)
			}

			if got := clarityGainDB(clarityParams{Mode: clarityModeOzone, Level: c.Panel}); math.Abs(got-c.WantDB) > 1e-9 {
				t.Errorf("clarityGainDB(OZONE, %g) = %g != registry value %g", c.Panel, got, c.WantDB)
			}
		case "viperbass_natural":

			wet := c.Panel / 100.0
			if got := 20 * math.Log10(1+wet); math.Abs(got-c.WantDB) > 1e-9 {
				t.Errorf("viperbass 1+g/100 = %g != registry value %g", got, c.WantDB)
			}
			if got := 20 * math.Log10(gainClassLinear(c.Panel)); math.Abs(got-c.WantDB) > 1e-9 {
				t.Errorf("gainClassLinear(%g) converted to dB = %g != registry value %g", c.Panel, got, c.WantDB)
			}
		case "vse_exciter":
			p, err := vseExciterParams(c.Panel)
			if err != nil {
				t.Fatalf("vseExciterParams(%g): %v", c.Panel, err)
			}
			if math.Abs(p.Mix-c.Wet) > 1e-12 {
				t.Errorf("VSE tier %g in-core wet is %g, registry says %g", c.Panel, p.Mix, c.Wet)
			}
			if got := 20 * math.Log10(p.Mix); math.Abs(got-c.WantDB) > 1e-9 {
				t.Errorf("VSE tier %g wet dB = %g != registry value %g", c.Panel, got, c.WantDB)
			}
		case "clarity_natural":

			g := c.Panel / 100.0
			if math.Abs(g-c.Wet) > 1e-12 {
				t.Errorf("Clarity NATURAL linear coefficient should be %g, got %g", c.Wet, g)
			}
		case "analogx_model":

			if got := analogxTiers[0].Gain; math.Abs(got-c.Wet) > 1e-12 {
				t.Errorf("AnalogX model 0 in-core gain should be %g, got %g", c.Wet, got)
			}
		}
	}
}

func TestA6NoUnregisteredLog10File(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), "math.Log10") {
			continue
		}
		if _, ok := panelDBLog10Files[f]; !ok {
			t.Errorf("%s uses math.Log10 but is not in panelDBLog10Files -- "+
				"decide which class it belongs to:\n"+
				"  - panel value -> dB => add this site to panelDBConventions in conventions.go"+
				"(and document the file here)\n"+
				"  - other dimensions (frequency response/headroom/IR gain...) => state the reason in panelDBLog10Files", f)
		}
	}

	for f, why := range panelDBLog10Files {
		if why == "" {
			t.Errorf("panelDBLog10Files entry %s states no reason", f)
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Errorf("panelDBLog10Files entry %s does not exist: %v", f, err)
			continue
		}
		if !strings.Contains(string(src), "math.Log10") {
			t.Errorf("panelDBLog10Files entry %s no longer uses math.Log10 -- please remove it from the table", f)
		}
	}
}

func TestClarityOzoneUsesSourceFormula(t *testing.T) {
	for _, level := range []float64{0, 10, 25, 50, 75, 100} {
		g := level / 100.0

		impl := 20 * math.Log10(g+1)

		cls := gainClassDB(level)
		if math.Abs(impl-cls) > 1e-9 {
			t.Errorf("Level=%g: source convention 20log10(g+1)=%g differs from gainClassDB=%g", level, impl, cls)
		}

		if level == 50 && math.Abs(impl-3.52) > 0.01 {
			t.Errorf("Level=50 OZONE gain should be ~= +3.52 dB, got %g", impl)
		}
	}

	if d := gainClassDB(50) - levelClassDB(50); math.Abs(d-9.54) > 0.05 {
		t.Errorf("gain/level class difference at 50 should be 9.54 dB, got %g", d)
	}
}

func TestA6DynamicBassBranchDecision(t *testing.T) {

	src := readRefs(t, refsDynamicBassCPP)
	if !strings.Contains(src, "this->lowFreqX <= 120") {
		t.Error("DynamicBass.cpp:21 should hold the `this->lowFreqX <= 120` branch criterion")
	}

	if !strings.Contains(src, "this->lowPass.ProcessSample(left + right)") {
		t.Error("DynamicBass.cpp:25 simple branch should only hold `lowPass.ProcessSample(left + right)`")
	}

	for _, want := range []string{"filterX.DoFilterLeft", "filterY.DoFilterRight", "this->sideGainX * y2"} {
		if !strings.Contains(src, want) {
			t.Errorf("DynamicBass.cpp:29-41 full branch should contain %q", want)
		}
	}

	if dynamicBassDefaultPresetX1 != 100 {
		t.Errorf("V4A default preset x1 should be 100, got %g", dynamicBassDefaultPresetX1)
	}
	if dynamicBassSimpleBranchMaxX1 != 120 {
		t.Errorf("Simple-branch threshold should be 120 (DynamicBass.cpp:21), got %g", dynamicBassSimpleBranchMaxX1)
	}
	if got := dynamicBassBranchOf(dynamicBassDefaultPresetX1); got != "simple" {
		t.Errorf("Default preset x1=100 should land on the simple branch, got %q", got)
	}

	arr := readRefs(t, refsArraysXML)
	if !strings.Contains(arr, "140;6200;40;60;10;80") {
		t.Log("Note: arrays.xml dynamicsystem_outputs_values first item should be 140;6200;...")
	}
	if got := dynamicBassBranchOf(140); got != "full" {
		t.Errorf("x1=140 (Extreme Headphone(v2)) should take the full branch, got %q", got)
	}

	if got := dynamicBassBranchOf(120); got != "simple" {
		t.Errorf("x1=120 should take the simple branch (source uses `<=`), got %q", got)
	}

	savedDB := currentDynamicBass
	t.Cleanup(func() { currentDynamicBass = savedDB })
	currentDynamicBass = nil
	note := dynamicBassBranchNote()
	if note["implemented"] != false {
		t.Error("When off, implemented should be false (this bit currently tracks DynamicBass on/off)")
	}
	if note["needs_owner_decision"] != false {
		t.Error("This item is decided as implemented (simple branch + full branch explicitly declined), so needs_owner_decision must be false")
	}
	if note["api_field"] != "dynamic_bass" {
		t.Errorf("Conclusion must point at the new API field, got %v", note["api_field"])
	}
	if note["branch_of_default_preset"] != "simple" {
		t.Errorf("Conclusion must pin branch_of_default_preset to the simple branch, got %v", note["branch_of_default_preset"])
	}
	if s, _ := note["decision"].(string); !strings.Contains(s, "simple branch") {
		t.Error("Conclusion must state the simple branch is done")
	}
	if s, _ := note["decision"].(string); !strings.Contains(s, "full branch") {
		t.Error("Conclusion must state the full branch is unimplemented (blocker = slots, not missing hardware)")
	}

	dbp := dynamicBassDefaultParams()
	currentDynamicBass = &dbp
	if dynamicBassBranchNote()["implemented"] != true {
		t.Error("When DynamicBass is on, implemented must be true")
	}
	currentDynamicBass = nil
	if s, _ := note["source"].(string); !strings.Contains(s, "DynamicBass.cpp:21-28") {
		t.Error("Conclusion must cite a primary source (DynamicBass.cpp:21-28)")
	}

	saved := currentDynBass
	t.Cleanup(func() { currentDynBass = saved })
	p := dynDefaultParams()
	currentDynBass = &p
	v, ok := dynBassView().(map[string]any)
	if !ok {
		t.Fatal("dynBassView should return a map")
	}
	if _, ok := v["v4a_dynamic_bass"]; !ok {
		t.Error("dynBassView should carry v4a_dynamic_bass (surfacing local DYN != V4A DynamicBass on the interface)")
	}
}

func TestA6LocalDynIsEnvelopeGainNotPolesFilter(t *testing.T) {

	if dynDetectFreq != 2200.0 || dynDetectQ != 0.33 {
		t.Errorf("Local DYN detection bandpass should be 2200 Hz / Q0.33, got %g / %g", dynDetectFreq, dynDetectQ)
	}
	sc := dynSideChainCoefs()
	mag := func(f float64) float64 { return cmplx.Abs(biquadResponse(sc, f, sampleRate)) }
	if mag(dynDetectFreq) <= mag(200) || mag(dynDetectFreq) <= mag(10000) {
		t.Errorf("Detection bandpass should peak at %g Hz: %.5f vs 200Hz %.5f / 10kHz %.5f",
			dynDetectFreq, mag(dynDetectFreq), mag(200), mag(10000))
	}

	if sc[1] != 0 {
		t.Errorf("RBJ bandpass b1 should be 0, got %d", sc[1])
	}

	p := dynDefaultParams()
	nodes, err := buildChainNodesWithBass(nil, &p, nil, nil, nil)
	if err != nil {
		t.Fatalf("Chain build failed: %v", err)
	}
	kinds := make([]string, 0, len(nodes))
	for _, n := range nodes {
		kinds = append(kinds, n.Kind)
	}
	want := []string{planKindDyn}
	if len(kinds) != 1 || kinds[0] != want[0] {
		t.Errorf("DYN should produce exactly one planKindDyn node (expanding to detection bandpass + DYN, two slots), got %v", kinds)
	}
	plan, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("Planning failed: %v", err)
	}
	if plan.Slots != 2 {
		t.Errorf("DYN should occupy 2 slots (detection bandpass + DYN), got %d", plan.Slots)
	}

	if p.GainDB == 0 && p.KS == 0 {
		t.Error("DYN params should be gain/cut/ref/ks/att/rel (slow level-following gain)")
	}

	typeOfDyn := reflectFieldsOf(dynParams{})
	for _, bad := range []string{"low_freq_x", "high_freq_x", "low_freq_y", "high_freq_y"} {
		if typeOfDyn[bad] {
			t.Errorf("dynParams contains %q -- those are V4A DynamicBass pole-filter params, "+
				"meaning local DYN was changed into a different algorithm (A6 item 3 conclusions must follow)", bad)
		}
	}
}

func reflectFieldsOf(v any) map[string]bool {
	out := map[string]bool{}
	rt := reflect.TypeOf(v)
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		name := f.Tag.Get("json")
		if i := strings.IndexByte(name, ','); i >= 0 {
			name = name[:i]
		}
		if name == "" {
			name = f.Name
		}
		out[name] = true
	}
	return out
}
