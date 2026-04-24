// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package validate

// Enumeration allowlists from Component Manifest v1 §7. All comparisons
// are case-sensitive per the preamble to §7.

var componentTypeValues = stringSet(
	"library", "application", "framework", "container",
	"operating-system", "device", "firmware", "file",
	"platform", "device-driver", "machine-learning-model", "data",
)

var scopeValues = stringSet(
	"required", "optional", "excluded",
)

var externalRefTypeValues = stringSet(
	"website", "vcs", "documentation", "issue-tracker",
	"distribution", "support", "release-notes", "advisories", "other",
)

var patchTypeValues = stringSet(
	"unofficial", "monkey", "backport", "cherry-pick",
)

var lifecyclePhaseValues = stringSet(
	"design", "pre-build", "build", "post-build",
	"operations", "discovery", "decommission",
)

func stringSet(values ...string) map[string]struct{} {
	s := make(map[string]struct{}, len(values))
	for _, v := range values {
		s[v] = struct{}{}
	}
	return s
}
