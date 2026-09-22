// Package contractflow contains historical contract handover metadata.
package contractflow

type History struct {
	RecordOnly   bool   `json:"recordOnly,omitempty"`
	TotalAmount  string `json:"totalAmount,omitempty"`
	Status       string `json:"status,omitempty"`
	TakeoverDate string `json:"takeoverDate"`
}
