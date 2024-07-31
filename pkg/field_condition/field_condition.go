package field_condition

type FieldCondition int

const (
	FieldEqual = iota
	FieldLike
	FieldBeginsWith
	FieldEndsWith
	FieldContains
)

func (f *FieldCondition) String() string {
	switch *f {
	case FieldEqual:
		return "EQUAL"
	case FieldLike:
		return "LIKE"
	case FieldBeginsWith:
		return "BEGINS_WITH"
	case FieldEndsWith:
		return "ENDS_WITH"
	case FieldContains:
		return "CONTAINS"
	default:
		return "UNKNOWN"
	}
}

func (f *FieldCondition) MarshalText() ([]byte, error) {
	return []byte(f.String()), nil
}

func (f *FieldCondition) UnmarshalText(text []byte) error {
	switch string(text) {
	case "EQUAL":
		*f = FieldEqual
	case "LIKE":
		*f = FieldLike
	case "BEGINS_WITH":
		*f = FieldBeginsWith
	case "ENDS_WITH":
		*f = FieldEndsWith
	case "CONTAINS":
		*f = FieldContains
	default:
		*f = FieldCondition(-1)
	}
	return nil
}
