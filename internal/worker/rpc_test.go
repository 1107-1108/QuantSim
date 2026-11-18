package worker

import "testing"

func TestTestRPC(t *testing.T) {
	// Arrange
	svc := &Service{}
	req := Request{A: 1, B: 2}
	var res Response

	// Act
	err := svc.TestRPC(req, &res)

	// Assert
	if err != nil {
		t.Fatalf("TestRPC returned unexpected error: %v", err)
	}
	if res.Res != 3 {
		t.Errorf("expected Res = 3, got %d", res.Res)
	}
}

func TestTestRPCWithNegativeNumbers(t *testing.T) {
	// Arrange
	svc := &Service{}
	req := Request{A: -5, B: 2}
	var res Response

	// Act
	err := svc.TestRPC(req, &res)

	// Assert
	if err != nil {
		t.Fatalf("TestRPC returned unexpected error: %v", err)
	}
	if res.Res != -3 {
		t.Errorf("expected Res = -3, got %d", res.Res)
	}
}
