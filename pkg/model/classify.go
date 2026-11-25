package model

import (
	"context"
	"errors"
)

// FailureKind is what a caller tells its user about a failed Run. Each
// caller maps it to its own answer, such as an HTTP status or a job error.
// Classify checks in this order: allowlist, audit trail, busy.
type FailureKind int

const (
	// FailureNone: err is nil.
	FailureNone FailureKind = iota
	// FailureProviderRefused: the provider allowlist refused the call; no
	// prompt was sent. Msg is the allowlist's reason, safe to show.
	FailureProviderRefused
	// FailureAuditFailed: the AI audit trail could not record the call.
	FailureAuditFailed
	// FailureBudgetSpent: today's token budget is spent.
	FailureBudgetSpent
	// FailureBudgetUnavailable: the token budget could not be read.
	FailureBudgetUnavailable
	// FailureCanceled: the caller's context was canceled.
	FailureCanceled
	// FailureBusy: 429, 5xx, a network error or a timeout.
	FailureBusy
	// FailureKeyRefused: 401 or 403 from the model API; the key or the
	// proxy is wrong.
	FailureKeyRefused
	// FailureNotFound: the model is not on the server (404).
	FailureNotFound
	// FailureModel: any other failed call (400, a backend's refusal).
	FailureModel
)

func (k FailureKind) String() string {
	switch k {
	case FailureNone:
		return "none"
	case FailureProviderRefused:
		return "provider_refused"
	case FailureAuditFailed:
		return "audit_failed"
	case FailureBudgetSpent:
		return "budget_spent"
	case FailureBudgetUnavailable:
		return "budget_unavailable"
	case FailureCanceled:
		return "canceled"
	case FailureBusy:
		return "busy"
	case FailureKeyRefused:
		return "key_refused"
	case FailureNotFound:
		return "not_found"
	}
	return "model"
}

// Failure is a classified error of Loop.Run or Model.Call.
type Failure struct {
	Kind FailureKind
	// Call is the call's error as a *CallError; nil for the budget kinds
	// and FailureCanceled. Its Msg may echo the request: redact it before
	// logging, and show it only for FailureProviderRefused.
	Call *CallError
}

// Classify sorts err into one Failure kind.
func Classify(err error) Failure {
	switch {
	case err == nil:
		return Failure{Kind: FailureNone}
	case errors.Is(err, ErrBudgetSpent):
		return Failure{Kind: FailureBudgetSpent}
	case errors.Is(err, ErrBudgetUnavailable):
		return Failure{Kind: FailureBudgetUnavailable}
	case errors.Is(err, context.Canceled):
		return Failure{Kind: FailureCanceled}
	}
	ce := AsCallError(err)
	switch {
	case errors.Is(err, ErrProviderNotAllowed):
		return Failure{Kind: FailureProviderRefused, Call: ce}
	case ce.AuditFailed:
		return Failure{Kind: FailureAuditFailed, Call: ce}
	}
	switch ce.Class {
	case Busy:
		return Failure{Kind: FailureBusy, Call: ce}
	case Refused:
		return Failure{Kind: FailureKeyRefused, Call: ce}
	case NotFound:
		return Failure{Kind: FailureNotFound, Call: ce}
	}
	return Failure{Kind: FailureModel, Call: ce}
}
