package linear

//go:generate go tool genqlient genqlient.yaml

// Input is a free-form GraphQL input object. It lets callers send only the
// fields they set, including explicit nulls, which typed structs cannot.
type Input map[string]any
