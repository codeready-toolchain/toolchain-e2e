package wait

import (
	"fmt"
	"strings"
)

// TierAndType parses the templateRef and extracts the `tier` and `type` values out of it
// returns an error if this TemplateRef's format is invalid
func TierAndType(templateRef string) (string, string, error) { // nolint:unparam
	// templateRef has format "<tier>-<type>-<based-on-tier-revision>-<template-revision>"
	subset := templateRef[0:strings.LastIndex(templateRef, "-")] // "<tier>-<type>-<based-on-tier-revision>"
	subset = templateRef[0:strings.LastIndex(subset, "-")]       // "<tier>-<type>"
	delimiterPos := strings.LastIndex(subset, "-")
	if delimiterPos == 0 || delimiterPos == len(subset) {
		return "", "", fmt.Errorf("invalid templateref: '%v'", templateRef)
	}
	return subset[0:delimiterPos], subset[delimiterPos+1:], nil // <tier> and <type>
}
