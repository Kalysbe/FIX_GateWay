package orders

import (
	"context"
	"errors"
	"testing"
)

func TestNewOrdersLimitedToServedClasses(t *testing.T) {
	ctx := context.Background()
	s := &Service{}
	if err := s.checkServedClass(ctx, "REPO"); err != nil {
		t.Fatal("no setting must accept every class", err)
	}
	s.SetServedClasses(func(context.Context) ([]string, error) { return []string{"LIST_GD", "LIST_GBA"}, nil })
	if err := s.checkServedClass(ctx, " LIST_GD "); err != nil {
		t.Fatal(err)
	}
	if err := s.checkServedClass(ctx, "REPO"); !errors.Is(err, ErrInstrumentNotFound) {
		t.Fatal("order of an unserved class accepted", err)
	}
	s.SetServedClasses(func(context.Context) ([]string, error) { return []string{}, nil })
	if err := s.checkServedClass(ctx, "REPO"); err != nil {
		t.Fatal("empty choice must accept every class", err)
	}
	down := errors.New("settings down")
	s.SetServedClasses(func(context.Context) ([]string, error) { return nil, down })
	if err := s.checkServedClass(ctx, "LIST_GD"); !errors.Is(err, down) {
		t.Fatal("settings failure accepted an order", err)
	}
}
