package domain

import "time"

type SetupStatus struct {
	Imported         bool       `json:"imported"`
	AssignmentsReady bool       `json:"assignmentsReady"`
	TimesReady       bool       `json:"timesReady"`
	SourceName       string     `json:"sourceName,omitempty"`
	ImportedAt       *time.Time `json:"importedAt,omitempty"`
}
