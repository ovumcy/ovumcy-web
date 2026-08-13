package services

import "testing"

// TestDayMoodScaleCoversTheValidatedRange pins the scale against the bounds the
// day-entry validator enforces. A surface renders the picker from this scale,
// so a scale that stopped one step short would silently drop a mood the
// validator still accepts.
func TestDayMoodScaleCoversTheValidatedRange(t *testing.T) {
	scale := DayMoodScale()

	if scale.Lowest != MinDayMood || scale.Highest != MaxDayMood {
		t.Fatalf("scale ends = %d..%d, want %d..%d", scale.Lowest, scale.Highest, MinDayMood, MaxDayMood)
	}
	if len(scale.Steps) != MaxDayMood-MinDayMood+1 {
		t.Fatalf("scale holds %d steps, want %d", len(scale.Steps), MaxDayMood-MinDayMood+1)
	}
	for offset, step := range scale.Steps {
		if step != MinDayMood+offset {
			t.Fatalf("step %d of the scale is %d, want %d (the scale is ordered, lowest first)", offset, step, MinDayMood+offset)
		}
		if !IsValidDayMood(step) {
			t.Fatalf("step %d is offered by the scale but rejected by the validator", step)
		}
	}
	if scale.Steps[0] != scale.Lowest || scale.Steps[len(scale.Steps)-1] != scale.Highest {
		t.Fatalf("scale ends %d..%d disagree with its own steps %v", scale.Lowest, scale.Highest, scale.Steps)
	}
}

// TestEveryMoodStepIsNamedAndDistinct is the finding this scale exists for: a
// step drawn as a face with no name leaves the reader guessing what it records.
// Every step therefore resolves to a catalogue key, and no two steps share one —
// two names that collide would put two different moods under one label.
func TestEveryMoodStepIsNamedAndDistinct(t *testing.T) {
	seenKeys := map[string]int{}
	seenFaces := map[string]int{}
	for _, step := range DayMoodScale().Steps {
		key := MoodTranslationKey(step)
		if key == "" {
			t.Fatalf("mood step %d has no name key", step)
		}
		if previous, duplicate := seenKeys[key]; duplicate {
			t.Fatalf("mood steps %d and %d share the name key %q", previous, step, key)
		}
		seenKeys[key] = step

		face := MoodEmoji(step)
		if face == "" {
			t.Fatalf("mood step %d has no face", step)
		}
		if previous, duplicate := seenFaces[face]; duplicate {
			t.Fatalf("mood steps %d and %d share the face %q", previous, step, face)
		}
		seenFaces[face] = step
	}

	// Off-scale values are not steps: they carry no name, and a caller renders
	// its own no-data label instead of an empty one.
	for _, offScale := range []int{MinDayMood - 1, MaxDayMood + 1} {
		if key := MoodTranslationKey(offScale); key != "" {
			t.Fatalf("off-scale mood %d resolved to name key %q, want none", offScale, key)
		}
	}
}
