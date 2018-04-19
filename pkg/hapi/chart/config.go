package chart

// Config supplies values to the parametrizable templates of a chart.
type Config struct {
	Raw    string                 `json:"raw,omitempty"`
	Values map[string]interface{} `json:"values,omitempty"`
}

// Values represents a collection of chart values.
type Values map[string]interface{}
