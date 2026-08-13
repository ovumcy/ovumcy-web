package services

func MoodEmoji(value int) string {
	switch value {
	case 1:
		return "😞"
	case 2:
		return "😕"
	case 3:
		return "😐"
	case 4:
		return "🙂"
	case 5:
		return "😊"
	default:
		return ""
	}
}

// MoodScale describes the steps a day entry may record its mood on. The bounds
// are MinDayMood/MaxDayMood, the same pair the input validator enforces, so a
// surface that renders the scale reads it here instead of repeating a count of
// its own — a picker that offers a step the validator rejects, or omits one it
// accepts, is the failure this type exists to make impossible.
type MoodScale struct {
	Steps   []int
	Lowest  int
	Highest int
}

// DayMoodScale returns the mood scale, lowest step first.
func DayMoodScale() MoodScale {
	steps := make([]int, 0, MaxDayMood-MinDayMood+1)
	for step := MinDayMood; step <= MaxDayMood; step++ {
		steps = append(steps, step)
	}
	return MoodScale{Steps: steps, Lowest: MinDayMood, Highest: MaxDayMood}
}
