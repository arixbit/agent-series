package tools

import "testing"

func TestEvalBasic(t *testing.T) {
	tests := []struct {
		expr string
		want float64
	}{
		{"1+2", 3},
		{"10-3", 7},
		{"4*5", 20},
		{"20/4", 5},
		{"-5+3", -2},
		{"(1+2)*3", 9},
		{"2*(3+4)", 14},
		{"10/(2+3)", 2},
		{"3.5+1.5", 5},
	}
	for _, tt := range tests {
		got, err := evalExpression(tt.expr)
		if err != nil {
			t.Errorf("evalExpression(%q) unexpected error: %v", tt.expr, err)
			continue
		}
		if got != tt.want {
			t.Errorf("evalExpression(%q) = %v, want %v", tt.expr, got, tt.want)
		}
	}
}

func TestEvalDivByZero(t *testing.T) {
	_, err := evalExpression("1/0")
	if err == nil {
		t.Fatal("expected division by zero error")
	}
}

func TestEvalEmpty(t *testing.T) {
	_, err := evalExpression("")
	if err == nil {
		t.Fatal("expected error for empty expression")
	}
}

func TestEvalInvalidExpressions(t *testing.T) {
	tests := []string{"1+", "*2", "2/(1-1)", "(1+2"}
	for _, expr := range tests {
		t.Run(expr, func(t *testing.T) {
			if _, err := evalExpression(expr); err == nil {
				t.Fatalf("evalExpression(%q) expected error", expr)
			}
		})
	}
}
