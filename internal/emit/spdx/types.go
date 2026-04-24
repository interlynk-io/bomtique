// SPDX-FileCopyrightText: 2026 Interlynk.io
// SPDX-License-Identifier: Apache-2.0

package spdx

// SPDX 2.3 JSON types — struct-per-field (no maps) so the emitted
// bytes are ordered deterministically. Only the subset §14.2 projects
// to is modelled; adding fields later means growing the struct, not
// rewriting the emitter.

const (
	spdxVersion    = "SPDX-2.3"
	dataLicenseCC0 = "CC0-1.0"
	documentSPDXID = "SPDXRef-DOCUMENT"

	relationshipDescribes = "DESCRIBES"
	relationshipDependsOn = "DEPENDS_ON"

	noAssertion = "NOASSERTION"
)

type spdxDocument struct {
	SPDXVersion       string         `json:"spdxVersion"`
	DataLicense       string         `json:"dataLicense"`
	SPDXID            string         `json:"SPDXID"`
	Name              string         `json:"name"`
	DocumentNamespace string         `json:"documentNamespace"`
	CreationInfo      *spdxCreation  `json:"creationInfo"`
	Packages          []spdxPackage  `json:"packages,omitempty"`
	Relationships     []spdxRelation `json:"relationships,omitempty"`
}

type spdxCreation struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators,omitempty"`
}

type spdxPackage struct {
	SPDXID                string            `json:"SPDXID"`
	Name                  string            `json:"name"`
	VersionInfo           string            `json:"versionInfo,omitempty"`
	DownloadLocation      string            `json:"downloadLocation"`
	FilesAnalyzed         *bool             `json:"filesAnalyzed,omitempty"`
	PrimaryPackagePurpose string            `json:"primaryPackagePurpose,omitempty"`
	Description           string            `json:"description,omitempty"`
	Supplier              string            `json:"supplier,omitempty"`
	Homepage              string            `json:"homepage,omitempty"`
	LicenseConcluded      string            `json:"licenseConcluded,omitempty"`
	LicenseDeclared       string            `json:"licenseDeclared,omitempty"`
	LicenseComments       string            `json:"licenseComments,omitempty"`
	Checksums             []spdxChecksum    `json:"checksums,omitempty"`
	ExternalRefs          []spdxExternalRef `json:"externalRefs,omitempty"`
	SourceInfo            string            `json:"sourceInfo,omitempty"`
	Comment               string            `json:"comment,omitempty"`
	Annotations           []spdxAnnotation  `json:"annotations,omitempty"`
}

type spdxChecksum struct {
	Algorithm     string `json:"algorithm"`
	ChecksumValue string `json:"checksumValue"`
}

type spdxExternalRef struct {
	ReferenceCategory string `json:"referenceCategory"`
	ReferenceType     string `json:"referenceType"`
	ReferenceLocator  string `json:"referenceLocator"`
	Comment           string `json:"comment,omitempty"`
}

type spdxAnnotation struct {
	AnnotationDate string `json:"annotationDate"`
	AnnotationType string `json:"annotationType"`
	Annotator      string `json:"annotator"`
	Comment        string `json:"comment"`
}

type spdxRelation struct {
	SPDXElementID      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSPDXElement string `json:"relatedSpdxElement"`
}
