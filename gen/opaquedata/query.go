package opaquedata

// SortOperator defines the comparison operator for sort key conditions.
type SortOperator int

const (
	Equal      SortOperator = iota
	Lt                      // <
	Le                      // <=
	Gt                      // >
	Ge                      // >=
	Between                 // BETWEEN value AND value2
	BeginsWith              // begins_with
)

// SortCondition describes a sort key filter for queries.
type SortCondition struct {
	Operator SortOperator
	Value    string
	Value2   string // only used by Between
}

// QueryOption configures a query.
type QueryOption func(*QueryOptions)

// QueryOptions holds resolved query parameters. Exported so store adapters
// can read them; callers should use the functional option constructors.
type QueryOptions struct {
	GSIIndex int // 0 = main table, 1-20 = GSI index
}

// WithGSIIndex directs the query to the specified GSI (1-20).
func WithGSIIndex(n int) QueryOption {
	return func(o *QueryOptions) { o.GSIIndex = n }
}

// ApplyQueryOptions resolves a slice of QueryOption into QueryOptions.
func ApplyQueryOptions(opts []QueryOption) QueryOptions {
	qo := QueryOptions{}
	for _, fn := range opts {
		fn(&qo)
	}
	return qo
}
