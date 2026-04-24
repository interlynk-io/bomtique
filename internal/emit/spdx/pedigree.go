// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package spdx

import (
	"fmt"
	"strings"

	"github.com/interlynk-io/bomtique/internal/manifest"
)

// applyPedigree projects a Component's pedigree onto SPDX per §14.2:
//
//   - ancestors → sourceInfo (one line per ancestor).
//   - commits → sourceInfo (appended after ancestors).
//   - notes → package.comment.
//   - patches → package.annotations (one per patch, annotator
//     "Tool: bomtique", type OTHER, content describes the patch).
//   - variants → dropped with a §14.2 warning.
//   - descendants → dropped with a §14.2 warning.
//
// `drops` accumulates per-class drop triggers so a single warning is
// emitted after every package is processed.
func applyPedigree(pkg *spdxPackage, p *manifest.Pedigree, drops *droppedCounter, annotationTimestamp string) {
	var info strings.Builder

	for _, a := range p.Ancestors {
		if info.Len() > 0 {
			info.WriteByte('\n')
		}
		info.WriteString(formatAncestorLine(&a))
	}
	if len(p.Commits) > 0 {
		if info.Len() > 0 {
			info.WriteByte('\n')
		}
		for i, c := range p.Commits {
			if i > 0 {
				info.WriteByte('\n')
			}
			info.WriteString(formatCommitLine(&c))
		}
	}
	if info.Len() > 0 {
		pkg.SourceInfo = info.String()
	}

	if p.Notes != nil && *p.Notes != "" {
		pkg.Comment = *p.Notes
	}

	for _, patch := range p.Patches {
		pkg.Annotations = append(pkg.Annotations, spdxAnnotation{
			AnnotationDate: annotationTimestamp,
			AnnotationType: "OTHER",
			Annotator:      toolCreator,
			Comment:        formatPatchAnnotation(&patch),
		})
	}

	if len(p.Variants) > 0 {
		drops.variants()
	}
	if len(p.Descendants) > 0 {
		drops.descendants()
	}
}

// formatAncestorLine builds the `sourceInfo` line for a single
// ancestor. §14.2 calls this approximated — SPDX's sourceInfo is free
// text so the format is "Ancestor: <name>@<version> (purl: <purl>)"
// with absent pieces skipped.
func formatAncestorLine(a *manifest.Component) string {
	parts := []string{"Ancestor:"}
	if a.Name != "" {
		if a.Version != nil && *a.Version != "" {
			parts = append(parts, fmt.Sprintf("%s@%s", a.Name, *a.Version))
		} else {
			parts = append(parts, a.Name)
		}
	}
	if a.Purl != nil && *a.Purl != "" {
		parts = append(parts, fmt.Sprintf("(purl: %s)", *a.Purl))
	}
	return strings.Join(parts, " ")
}

// formatCommitLine renders one commit entry into a single sourceInfo
// line. Both fields are optional; blank lines are skipped.
func formatCommitLine(c *manifest.Commit) string {
	parts := []string{"Commit:"}
	if c.UID != nil && *c.UID != "" {
		parts = append(parts, *c.UID)
	}
	if c.URL != nil && *c.URL != "" {
		parts = append(parts, *c.URL)
	}
	return strings.Join(parts, " ")
}

// formatPatchAnnotation builds the annotation comment body for a
// single patch.  Keep the body short — it's a free-text SPDX field
// with no structural rules, but we want it readable.
func formatPatchAnnotation(p *manifest.Patch) string {
	var b strings.Builder
	b.WriteString("patch type: " + p.Type)
	if p.Diff != nil {
		switch {
		case p.Diff.URL != nil && *p.Diff.URL != "":
			b.WriteString("; source: " + *p.Diff.URL)
		case p.Diff.Text != nil:
			b.WriteString("; source: inline text")
		}
	}
	for _, r := range p.Resolves {
		b.WriteString("; resolves:")
		if r.Type != nil && *r.Type != "" {
			b.WriteString(" " + *r.Type)
		}
		if r.Name != nil && *r.Name != "" {
			b.WriteString(" " + *r.Name)
		}
		if r.URL != nil && *r.URL != "" {
			b.WriteString(" (" + *r.URL + ")")
		}
	}
	return b.String()
}
