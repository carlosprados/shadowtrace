// Package anomaly performs Isolation Forest *inference* in Go over a model that
// was trained and exported to JSON by the Python trainer (tools/train.py). The
// feature vector and math mirror scikit-learn so scores match the trainer.
package anomaly

import (
	"encoding/json"
	"math"
	"os"
	"time"
)

// FeatureNames documents the vector; keep in lockstep with tools/train.py.
var FeatureNames = []string{"hour_sin", "hour_cos", "dow_sin", "dow_cos", "rssi", "known"}

// Tree is one ExtraTree from sklearn's estimators_ (arrays indexed by node).
type Tree struct {
	Feature   []int     `json:"feature"` // -2 = leaf
	Threshold []float64 `json:"threshold"`
	Left      []int     `json:"left"`
	Right     []int     `json:"right"`
	NNode     []int     `json:"n_node_samples"`
}

// Model is the exported Isolation Forest.
type Model struct {
	FeatureNames []string  `json:"feature_names"`
	Mean         []float64 `json:"mean"`  // StandardScaler.mean_
	Scale        []float64 `json:"scale"` // StandardScaler.scale_
	MaxSamples   int       `json:"max_samples"`
	Offset       float64   `json:"offset"` // IsolationForest.offset_
	Trees        []Tree    `json:"trees"`
	TrainedAt    string    `json:"trained_at"`
	NTrain       int       `json:"n_train"`
}

const euler = 0.5772156649015329

// c is the average path length of an unsuccessful BST search over n points.
func c(n int) float64 {
	if n <= 1 {
		return 0
	}
	h := math.Log(float64(n-1)) + euler
	return 2*h - 2*float64(n-1)/float64(n)
}

// Features builds the model input for one sighting. weekday uses Monday=0 to
// match Python's datetime.weekday().
func Features(ts time.Time, rssi float64, known bool) []float64 {
	local := ts.Local()
	hour := float64(local.Hour()) + float64(local.Minute())/60.0
	dow := float64((int(local.Weekday()) + 6) % 7)
	k := 0.0
	if known {
		k = 1.0
	}
	return []float64{
		math.Sin(2 * math.Pi * hour / 24.0),
		math.Cos(2 * math.Pi * hour / 24.0),
		math.Sin(2 * math.Pi * dow / 7.0),
		math.Cos(2 * math.Pi * dow / 7.0),
		rssi,
		k,
	}
}

// LoadModel reads an exported model from disk.
func LoadModel(path string) (*Model, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Model
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Model) scale(x []float64) []float64 {
	out := make([]float64, len(x))
	for i := range x {
		s := 1.0
		if i < len(m.Scale) && m.Scale[i] != 0 {
			s = m.Scale[i]
		}
		mean := 0.0
		if i < len(m.Mean) {
			mean = m.Mean[i]
		}
		out[i] = (x[i] - mean) / s
	}
	return out
}

func treePath(t Tree, x []float64) float64 {
	node, depth := 0, 0.0
	for t.Feature[node] >= 0 {
		f := t.Feature[node]
		if f < len(x) && x[f] <= t.Threshold[node] {
			node = t.Left[node]
		} else {
			node = t.Right[node]
		}
		depth++
	}
	return depth + c(t.NNode[node])
}

// Decision returns sklearn's decision_function value for a raw feature vector:
// lower = more anomalous; < 0 means "outlier" under the trained contamination.
func (m *Model) Decision(x []float64) float64 {
	if len(m.Trees) == 0 {
		return 0
	}
	xs := m.scale(x)
	sum := 0.0
	for _, t := range m.Trees {
		sum += treePath(t, xs)
	}
	avg := sum / float64(len(m.Trees))
	s := math.Pow(2, -avg/c(m.MaxSamples))
	return -s - m.Offset
}
