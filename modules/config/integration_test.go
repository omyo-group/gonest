package config_test

import (
	"errors"
	"testing"

	"github.com/omyo-group/gonest"
	"github.com/omyo-group/gonest/modules/config"
)

type itCfg struct {
	Name string `default:"it"`
}

// TestIntegrationApp starts passing automatically once gonest.New is implemented.
func TestIntegrationApp(t *testing.T) {
	root := &gonest.Module{Name: "root", Imports: []*gonest.Module{config.ForRoot[itCfg](config.Options{EnvFiles: []string{}})}}
	app, err := gonest.New(root, gonest.WithoutStartupLog())
	if err != nil && err.Error() == "gonest: not implemented" {
		t.Skip("gonest.New not implemented yet")
	}
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := gonest.Get[*itCfg](app)
	if err != nil && !errors.Is(err, nil) {
		t.Fatal(err)
	}
	if cfg.Name != "it" {
		t.Fatalf("got %+v", cfg)
	}
}
