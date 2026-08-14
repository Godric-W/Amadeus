package policy

import "context"

type ApprovalPort interface {
	Decide(context.Context, ApprovalRequest) (ApprovalDecision, error)
}
