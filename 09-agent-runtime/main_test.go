package main

import "testing"

func TestEvalExpression(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want float64
	}{
		{name: "addition", expr: "1+2", want: 3},
		{name: "precedence", expr: "2+3*4", want: 14},
		{name: "parentheses", expr: "(1+2)*3", want: 9},
		{name: "unary minus", expr: "-5+3", want: -2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := evalExpression(tt.expr)
			if err != nil {
				t.Fatalf("evalExpression(%q) error: %v", tt.expr, err)
			}
			if got != tt.want {
				t.Fatalf("evalExpression(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}

func TestEvalExpressionRejectsInvalidInput(t *testing.T) {
	tests := []string{"", "1+", "2/0", "(1+2"}
	for _, expr := range tests {
		t.Run(expr, func(t *testing.T) {
			if _, err := evalExpression(expr); err == nil {
				t.Fatalf("evalExpression(%q) expected error", expr)
			}
		})
	}
}
