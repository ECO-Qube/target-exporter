package pyzhm

import (
	"bytes"
	"go.uber.org/zap"
	"io"
	"k8s.io/apimachinery/pkg/util/json"
	"net/http"
)

const TestScenarioJson = `
{
  "scenario": {
    "L1": "163.47",
    "L3": "207.79",
    "L5": "144.51",
    "L7": "202.62",
    "L9": "187.44",
    "L11": "195.54",
    "L13": "208.63",
    "L15": "165.79",
    "L17": "179.72",
    "L19": "150.8",
    "L21": "193.27",
    "L23": "188.43",
    "R1": "73.1",
    "R3": "69",
    "R5": "69",
    "R7": "69",
    "R9": "69",
    "R11": "69",
    "R13": "69",
    "R15": "69",
    "R17": "69",
    "R19": "69",
    "R21": "69",
    "R23": "69"
  },
  "requirements": {
    "job1": "1",
  }
}
`

type PyzhmClient struct {
	logger *zap.Logger
}

type NodeLabel string

type InstantPowerUsage struct {
	// NodeLabel -> InstantPowerUsage
	Usage float64
}

type Scenario struct {
	// NodeLabel -> InstantPowerUsage
	Scenario     map[string]float64 `json:"scenario"`
	Requirements map[string]float64 `json:"requirements"`
}

type Predictions struct {
	Assignments map[string]NodeLabel `json:"assignments"`
}

// ScenarioReader is an interface for reading scenarios from a CSV file.
//type ScenarioReader interface {
//	Read() ([]ScenarioPayload, error)
//}

func NewPyzhmClient() *PyzhmClient {
	return &PyzhmClient{}
}

func (p *PyzhmClient) Predict(scenario Scenario) (Predictions, error) {
	// Marshal scenario into JSON and send post request to pyzhm
	payload, err := json.Marshal(scenario)
	if err != nil {
		p.logger.Error("failed to marshal scenario", zap.Error(err))
		return Predictions{}, err
	}
	payloadReader := bytes.NewReader(payload)
	resp, err := http.Post("http://pyzhm.pyzhm.svc.cluster.local:5001/predict", "application/json", payloadReader)
	if err != nil {
		p.logger.Error("failed to send post request to pyzhm", zap.Error(err))
		return Predictions{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		p.logger.Error("failed to read response body from pyzhm", zap.Error(err))
		return Predictions{}, err
	}
	// Unmarshal response into Predictions
	var predictions Predictions
	err = json.Unmarshal(respBody, &predictions)
	if err != nil {
		p.logger.Error("failed to unmarshal response body from pyzhm", zap.Error(err))
		return Predictions{}, err
	}

	return predictions, nil
}

func (p *PyzhmClient) GetTestScenario() (Scenario, error) {
	// Unmarshal test scenario into Scenario
	var scenario Scenario
	err := json.Unmarshal([]byte(TestScenarioJson), &scenario)
	if err != nil {
		p.logger.Error("failed to unmarshal test scenario", zap.Error(err))
		return Scenario{}, err
	}

	return scenario, nil
}
