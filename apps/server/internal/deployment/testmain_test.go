package deployment

import (
	"os"
	"testing"

	"github.com/chaos-plus/chaosplus/internal/infra/guid"
)

func TestMain(m *testing.M) {
	generator, err := guid.New(0)
	if err != nil {
		panic(err)
	}
	guid.SetDefault(generator)
	os.Exit(m.Run())
}
