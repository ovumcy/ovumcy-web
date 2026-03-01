package services

import "fmt"

func BuildCycleTrendLabels(cycleLabelPattern string, pointCount int) []string {
	if pointCount <= 0 {
		return []string{}
	}
	if cycleLabelPattern == "" {
		cycleLabelPattern = "Cycle %d"
	}

	labels := make([]string, 0, pointCount)
	for index := 0; index < pointCount; index++ {
		labels = append(labels, fmt.Sprintf(cycleLabelPattern, index+1))
	}
	return labels
}
