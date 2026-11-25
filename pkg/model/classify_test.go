package model

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassify(t *testing.T) {
	refused := fmt.Errorf("%w: %w", ErrProviderNotAllowed, &CallError{Class: Bad, Msg: "add it to providers.yaml"})
	tests := []struct {
		name string
		err  error
		want FailureKind
	}{
		{"nil", nil, FailureNone},
		{"allowlist refusal", refused, FailureProviderRefused},
		{"audit start row failed (busy class)", &CallError{Class: Busy, AuditFailed: true}, FailureAuditFailed},
		{"audit end row failed (bad class)", &CallError{Class: Bad, AuditFailed: true}, FailureAuditFailed},
		{"busy", &CallError{Class: Busy, Status: 503}, FailureBusy},
		{"key refused", &CallError{Class: Refused, Status: 401}, FailureKeyRefused},
		{"model not found", &CallError{Class: NotFound, Status: 404}, FailureNotFound},
		{"bad request", &CallError{Class: Bad, Status: 400}, FailureModel},
		{"wrapped call error", fmt.Errorf("model call: %w", &CallError{Class: NotFound}), FailureNotFound},
		{"plain error is busy, as AsCallError", errors.New("dial tcp: refused"), FailureBusy},
		{"budget spent", ErrBudgetSpent, FailureBudgetSpent},
		{"budget unavailable", &BudgetError{Err: errors.New("db down")}, FailureBudgetUnavailable},
		{"canceled", context.Canceled, FailureCanceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := Classify(tt.err)
			assert.Equal(t, tt.want, f.Kind, f.Kind.String())
		})
	}
}

func TestClassify_ProviderRefusedKeepsTheReason(t *testing.T) {
	err := fmt.Errorf("%w: %w", ErrProviderNotAllowed, &CallError{Class: Bad, Msg: "add it to providers.yaml"})
	f := Classify(err)
	if assert.NotNil(t, f.Call) {
		assert.Equal(t, "add it to providers.yaml", f.Call.Msg)
	}
}
