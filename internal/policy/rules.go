package policy

type Rule struct {
	Risk       string
	Tool       string
	Permission DecisionAction
}
