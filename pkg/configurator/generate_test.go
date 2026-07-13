package configurator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type genInner struct {
	Level string `mapstructure:"level" description:"log level" default:"info"`
}

type genSample struct {
	Name       string              `mapstructure:"name" description:"app name" default:"app"`
	Port       int                 `mapstructure:"port" default:"8080"`
	Count      int                 `mapstructure:"count"` // no default -> zero
	Debug      bool                `mapstructure:"debug" default:"false"`
	Timeout    time.Duration       `mapstructure:"timeout" default:"30m"`
	Addrs      []string            `mapstructure:"addrs"`
	Log        genInner            `mapstructure:"log"`
	Ptr        *genInner           `mapstructure:"ptr"` // pointer -> dereferenced
	Sources    map[string]genInner `mapstructure:"sources" mapkey:"<srckey>"`
	Tags       map[string]string   `mapstructure:"tags"` // scalar-valued map
	Secret     string              `mapstructure:"secret" hidden:"true"`
	unexported int                 //nolint:unused
	NoTag      string
}

func TestGenerateYAML_RoundTripsWithDefaults(t *testing.T) {
	data, err := GenerateYAML(&genSample{})
	require.NoError(t, err)

	var out genSample
	require.NoError(t, yaml.Unmarshal(data, &out))

	assert.Equal(t, "app", out.Name)
	assert.Equal(t, 8080, out.Port)
	assert.False(t, out.Debug)
	assert.Equal(t, "info", out.Log.Level)
	require.Contains(t, out.Sources, "srckey", "map placeholder entry uses the cleaned mapkey")
	assert.Equal(t, "info", out.Sources["srckey"].Level)
}

func TestGenerateYAML_OmitsHiddenAndUntagged(t *testing.T) {
	data, err := GenerateYAML(&genSample{})
	require.NoError(t, err)
	s := string(data)
	assert.NotContains(t, s, "secret", "hidden fields are omitted")
	assert.NotContains(t, s, "NoTag", "fields without a mapstructure tag are omitted")
}

func TestGenerateYAML_DurationIsString(t *testing.T) {
	data, err := GenerateYAML(&genSample{})
	require.NoError(t, err)
	assert.Contains(t, string(data), "timeout: 30m", "duration renders from its default tag")
}

func TestGenerateYAML_IncludesDescriptionComments(t *testing.T) {
	data, err := GenerateYAML(&genSample{})
	require.NoError(t, err)
	assert.Contains(t, string(data), "# app name", "description tags become comments")
}

func TestGenerateYAML_RejectsNonStruct(t *testing.T) {
	_, err := GenerateYAML(42)
	assert.Error(t, err)
}

func TestGenerateYAML_ZeroAndPointerFields(t *testing.T) {
	data, err := GenerateYAML(&genSample{})
	require.NoError(t, err)
	var out genSample
	require.NoError(t, yaml.Unmarshal(data, &out))
	assert.Equal(t, 0, out.Count, "field without a default renders its zero value")
	require.NotNil(t, out.Ptr, "pointer field is dereferenced and populated")
	assert.Equal(t, "info", out.Ptr.Level)
}

func TestGenerateYAML_RejectsUnsupportedField(t *testing.T) {
	type bad struct {
		C chan int `mapstructure:"c"`
	}
	_, err := GenerateYAML(&bad{})
	assert.Error(t, err, "an unsupported field type is reported")
}

func TestGenerateYAML_RejectsUnsupportedMapElem(t *testing.T) {
	type badMap struct {
		M map[string]chan int `mapstructure:"m"`
	}
	_, err := GenerateYAML(&badMap{})
	assert.Error(t, err, "an unsupported map element type is reported")
}
