// Copyright 2020 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package process

import (
	"errors"
	"fmt"
	"testing"

	"github.com/newrelic/infrastructure-agent/pkg/sysinfo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/newrelic/infrastructure-agent/internal/agent"
	"github.com/newrelic/infrastructure-agent/internal/agent/mocks"
	"github.com/newrelic/infrastructure-agent/pkg/config"
	"github.com/newrelic/infrastructure-agent/pkg/entity"
	"github.com/newrelic/infrastructure-agent/pkg/metrics"
	"github.com/newrelic/infrastructure-agent/pkg/plugins/ids"
	"github.com/newrelic/infrastructure-agent/pkg/sample"
	"github.com/newrelic/infrastructure-agent/pkg/sysinfo/hostname"
)

func TestProcessSampler_DockerDecorator(t *testing.T) {
	// Given a Process Sampler
	ctx := new(mocks.AgentContext)
	ctx.On("Config").Return(&config.Config{})
	ctx.On("GetServiceForPid", mock.Anything).Return("", false)
	ps := NewProcessSampler(ctx).(*processSampler)
	ps.harvest = &harvesterMock{samples: map[int32]*metrics.ProcessSample{
		1: {
			ProcessID:          1,
			ProcessDisplayName: "Hello",
		},
		2: {
			ProcessID:          2,
			ProcessDisplayName: "Bye",
		},
	}}
	ps.containerSampler = &fakeContainerSampler{}

	// When asking for the process samples
	samples, err := ps.Sample()
	require.NoError(t, err)

	// They are returned, decorated and normalized
	require.Len(t, samples, 2)

	for i := range samples {
		sample := samples[i].(*FlatProcessSample)
		switch int32((*sample)["processId"].(float64)) {
		case 1:
			assert.Equal(t, "Hello", (*sample)["processDisplayName"])
		case 2:
			assert.Equal(t, "Bye", (*sample)["processDisplayName"])
		default:
			assert.Failf(t, fmt.Sprintf("Unknown process: %#v", *sample), "")
		}
		assert.Equal(t, "decorated", (*sample)["containerImage"])
		assert.Equal(t, "value1", (*sample)["containerLabel_label1"])
		assert.Equal(t, "value2", (*sample)["containerLabel_label2"])
	}
}

type harvesterMock struct {
	samples map[int32]*metrics.ProcessSample
}

func (hm *harvesterMock) Pids() ([]int32, error) {
	keys := make([]int32, 0)
	for k := range hm.samples {
		keys = append(keys, k)
	}
	return keys, nil
}

func (hm *harvesterMock) Do(pid int32, elapsedSeconds float64) (*metrics.ProcessSample, error) {
	return hm.samples[pid], nil
}

type fakeContainerSampler struct{}

func (cs *fakeContainerSampler) Enabled() bool {
	return true
}

func (*fakeContainerSampler) NewDecorator() (metrics.ProcessDecorator, error) {
	return &fakeDecorator{}, nil
}

type fakeDecorator struct{}

func (pd *fakeDecorator) Decorate(process *metrics.ProcessSample) {
	process.ContainerImage = "decorated"
	process.ContainerLabels = map[string]string{
		"label1": "value1",
		"label2": "value2",
	}
}

func BenchmarkProcessSampler(b *testing.B) {
	pm := NewProcessSampler(&dummyAgentContext{})

	for i := 0; i < b.N; i++ {
		_, _ = pm.Sample()
	}
}

// Tests procs monitor without the Docker container metadata cache
func BenchmarkProcessSampler_NoCache(b *testing.B) {
	pm := NewProcessSampler(&dummyAgentContext{&config.Config{
		ContainerMetadataCacheLimit: -5,
	}})

	for i := 0; i < b.N; i++ {
		_, _ = pm.Sample()
	}
}

// DummyAgentContext replaces mock agent context because mocks management can have impact in benchmarks
type dummyAgentContext struct {
	cfg *config.Config
}

func (*dummyAgentContext) ActiveEntitiesChannel() chan string {
	return nil
}

func (*dummyAgentContext) AddReconnecting(agent.Plugin) {}

func (*dummyAgentContext) AgentIdentifier() string {
	return ""
}

func (*dummyAgentContext) CacheServicePids(source string, pidMap map[int]string) {}

func (d *dummyAgentContext) Config() *config.Config {
	return d.cfg
}

func (*dummyAgentContext) GetServiceForPid(pid int) (service string, ok bool) {
	return "", false
}

func (*dummyAgentContext) HostnameResolver() hostname.Resolver {
	return nil
}

func (*dummyAgentContext) Reconnect() {}

func (*dummyAgentContext) SendData(agent.PluginOutput) {}

func (*dummyAgentContext) SendEvent(event sample.Event, entityKey entity.Key) {}

func (*dummyAgentContext) Unregister(ids.PluginID) {}

func (*dummyAgentContext) Version() string {
	return ""
}

func (dummyAgentContext) IDLookup() agent.IDLookup {
	idLookupTable := make(agent.IDLookup)
	idLookupTable[sysinfo.HOST_SOURCE_HOSTNAME_SHORT] = "short_hostname"
	return idLookupTable
}

func Test_checkContainerNotRunning(t *testing.T) {
	type args struct {
		err error
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "match",
			args: args{err: errors.New("Error response from daemon: Container e9c57d578de9e487f6f703d04b1b237b1ff3d926d9cc2a4adfcbe8e1946e841f is not running")},
			want: "e9c57d578de9e487f6f703d04b1b237b1ff3d926d9cc2a4adfcbe8e1946e841f",
		},
		{
			name: "match2",
			args: args{err: errors.New("Error response from daemon: Container cb33a2dfaa4b25dddcd509b434bc6cd6c088a4e39a2611776d45fdb02b763039 is not running")},
			want: "cb33a2dfaa4b25dddcd509b434bc6cd6c088a4e39a2611776d45fdb02b763039",
		},
		{
			name: "nomatch",
			args: args{err: errors.New("not legit")},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containerIDFromNotRunningErr(tt.args.err); got != tt.want {
				t.Errorf("check() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Benchmark_checkContainerNotRunning(b *testing.B) {
	err := errors.New("Error response from daemon: Container e9c57d578de9e487f6f703d04b1b237b1ff3d926d9cc2a4adfcbe8e1946e841f is not running")
	for i := 0; i < b.N; i++ {
		if id := containerIDFromNotRunningErr(err); id != "e9c57d578de9e487f6f703d04b1b237b1ff3d926d9cc2a4adfcbe8e1946e841f" {
			b.Fatalf("check() = %s, want %s", id, "e9c57d578de9e487f6f703d04b1b237b1ff3d926d9cc2a4adfcbe8e1946e841f")
		}
	}
}