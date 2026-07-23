package anomaly

import (
	"math"
	"testing"
	"time"
)

func TestCZeroForSmall(t *testing.T) {
	if c(0) != 0 || c(1) != 0 {
		t.Fatal("c(n) must be 0 for n<=1")
	}
	if c(256) <= 0 {
		t.Fatal("c(256) must be positive")
	}
}

func TestFeaturesWeekdayMondayZero(t *testing.T) {
	// 2024-01-01 is a Monday -> Python weekday 0 -> dow_sin=0, dow_cos=1.
	f := Features(time.Date(2024, 1, 1, 12, 0, 0, 0, time.Local), -70, true)
	if len(f) != 6 {
		t.Fatalf("want 6 features, got %d", len(f))
	}
	if math.Abs(f[2]) > 1e-9 || math.Abs(f[3]-1) > 1e-9 {
		t.Errorf("Monday should give dow_sin=0,dow_cos=1; got %v,%v", f[2], f[3])
	}
	if f[4] != -70 || f[5] != 1 {
		t.Errorf("rssi/known passthrough wrong: %v,%v", f[4], f[5])
	}
}

func TestDecisionSingleLeafMatchesFormula(t *testing.T) {
	// One leaf tree: path length = c(maxSamples); avg/c(max)=1; s=2^-1=0.5.
	// decision = -s - offset = -0.5 - (-0.5) = 0.
	m := &Model{
		MaxSamples: 256,
		Offset:     -0.5,
		Mean:       []float64{0, 0, 0, 0, 0, 0},
		Scale:      []float64{1, 1, 1, 1, 1, 1},
		Trees: []Tree{{
			Feature: []int{-2}, Threshold: []float64{-2},
			Left: []int{-1}, Right: []int{-1}, NNode: []int{256},
		}},
	}
	got := m.Decision([]float64{0, 0, 0, 0, -70, 1})
	if math.Abs(got) > 1e-9 {
		t.Fatalf("want decision ~0, got %v", got)
	}
}

func TestDecisionRoutesThroughSplit(t *testing.T) {
	// Root splits on feature 4 (rssi) at threshold 0: left leaf deep (n=2),
	// right leaf shallow (n=200). A high rssi routes right (more "normal").
	m := &Model{
		MaxSamples: 256, Offset: -0.5,
		Mean: make([]float64, 6), Scale: []float64{1, 1, 1, 1, 1, 1},
		Trees: []Tree{{
			Feature:   []int{4, -2, -2},
			Threshold: []float64{0, -2, -2},
			Left:      []int{1, -1, -1},
			Right:     []int{2, -1, -1},
			NNode:     []int{202, 2, 200},
		}},
	}
	low := m.Decision([]float64{0, 0, 0, 0, -10, 0}) // <=0 -> left (short path, anomalous)
	high := m.Decision([]float64{0, 0, 0, 0, 10, 0}) // >0 -> right (long path, normal)
	if !(low < high) {
		t.Fatalf("expected low(%v) < high(%v)", low, high)
	}
}
