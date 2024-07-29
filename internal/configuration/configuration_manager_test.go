package configuration

import (
	"testing"

	"github.com/docker/docker/client"
	"github.com/krystofrezac/lifebuoy/internal/apps"
)

func TestCheckAppsNameCollisions_UniqueNames(t *testing.T) {
	appCreator := apps.NewDockefileAppCreator(nil, &client.Client{})
	c := ConfigurationManager{apps: []apps.App{
		appCreator.Create(apps.DockefileAppCreateOpts{AppName: "app-1"}),
		appCreator.Create(apps.DockefileAppCreateOpts{AppName: "app-2"}),
		appCreator.Create(apps.DockefileAppCreateOpts{AppName: "app-3"}),
	}}

	if c.checkAppsNameCollisions() != nil {
		t.Fatal("Expected nil")
	}
}

func TestCheckAppsNameCollisions_SameNames(t *testing.T) {
	appCreator := apps.NewDockefileAppCreator(nil, &client.Client{})
	c := ConfigurationManager{apps: []apps.App{
		appCreator.Create(apps.DockefileAppCreateOpts{AppName: "app-1"}),
		appCreator.Create(apps.DockefileAppCreateOpts{AppName: "app-2"}),
		appCreator.Create(apps.DockefileAppCreateOpts{AppName: "app-1"}),
	}}

	res := c.checkAppsNameCollisions()
	if res == nil {
		t.Fatal("Expected error")
	}

	exptedErrorMsg := "There are multiple apps with the same name. Duplicate names [app-1]"
	if res.Error() != exptedErrorMsg {
		t.Fatalf("Expected error '%s'", exptedErrorMsg)
	}
}
