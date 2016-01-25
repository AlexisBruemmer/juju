// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package controller_test

import (
	"fmt"
	"time"

	"github.com/juju/names"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/api"
	"github.com/juju/juju/api/base"
	"github.com/juju/juju/api/controller"
	commontesting "github.com/juju/juju/apiserver/common/testing"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/juju"
	jujutesting "github.com/juju/juju/juju/testing"
	"github.com/juju/juju/state"
	"github.com/juju/juju/state/multiwatcher"
	"github.com/juju/juju/testing"
	"github.com/juju/juju/testing/factory"
)

type controllerSuite struct {
	jujutesting.JujuConnSuite
	commontesting.BlockHelper
}

var _ = gc.Suite(&controllerSuite{})

func (s *controllerSuite) SetUpTest(c *gc.C) {
	s.JujuConnSuite.SetUpTest(c)
}

func (s *controllerSuite) OpenAPI(c *gc.C) *controller.Client {
	conn, err := juju.NewAPIState(s.AdminUserTag(c), s.Environ, api.DialOpts{})
	c.Assert(err, jc.ErrorIsNil)
	s.AddCleanup(func(*gc.C) { conn.Close() })
	return controller.NewClient(conn)
}

func (s *controllerSuite) TestAllModels(c *gc.C) {
	owner := names.NewUserTag("user@remote")
	s.Factory.MakeEnvironment(c, &factory.EnvParams{
		Name: "first", Owner: owner}).Close()
	s.Factory.MakeEnvironment(c, &factory.EnvParams{
		Name: "second", Owner: owner}).Close()

	sysManager := s.OpenAPI(c)
	envs, err := sysManager.AllModels()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(envs, gc.HasLen, 3)

	var obtained []string
	for _, env := range envs {
		obtained = append(obtained, fmt.Sprintf("%s/%s", env.Owner, env.Name))
	}
	expected := []string{
		"dummy-admin@local/dummymodel",
		"user@remote/first",
		"user@remote/second",
	}
	c.Assert(obtained, jc.SameContents, expected)
}

func (s *controllerSuite) TestEnvironmentConfig(c *gc.C) {
	sysManager := s.OpenAPI(c)
	env, err := sysManager.EnvironmentConfig()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(env["name"], gc.Equals, "dummymodel")
}

func (s *controllerSuite) TestDestroyController(c *gc.C) {
	s.Factory.MakeEnvironment(c, &factory.EnvParams{Name: "foo"}).Close()

	sysManager := s.OpenAPI(c)
	err := sysManager.DestroyController(false)
	c.Assert(err, gc.ErrorMatches, "controller model cannot be destroyed before all other models are destroyed")
}

func (s *controllerSuite) TestListBlockedModels(c *gc.C) {
	err := s.State.SwitchBlockOn(state.ChangeBlock, "change block for state server")
	err = s.State.SwitchBlockOn(state.DestroyBlock, "destroy block for state server")
	c.Assert(err, jc.ErrorIsNil)

	sysManager := s.OpenAPI(c)
	results, err := sysManager.ListBlockedModels()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results, jc.DeepEquals, []params.ModelBlockInfo{
		params.ModelBlockInfo{
			Name:     "dummymodel",
			UUID:     s.State.EnvironUUID(),
			OwnerTag: s.AdminUserTag(c).String(),
			Blocks: []string{
				"BlockChange",
				"BlockDestroy",
			},
		},
	})
}

func (s *controllerSuite) TestRemoveBlocks(c *gc.C) {
	s.State.SwitchBlockOn(state.DestroyBlock, "TestBlockDestroyModel")
	s.State.SwitchBlockOn(state.ChangeBlock, "TestChangeBlock")

	sysManager := s.OpenAPI(c)
	err := sysManager.RemoveBlocks()
	c.Assert(err, jc.ErrorIsNil)

	blocks, err := s.State.AllBlocksForController()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(blocks, gc.HasLen, 0)
}

func (s *controllerSuite) TestWatchAllEnvs(c *gc.C) {
	// The WatchAllEnvs infrastructure is comprehensively tested
	// else. This test just ensure that the API calls work end-to-end.
	sysManager := s.OpenAPI(c)

	w, err := sysManager.WatchAllEnvs()
	c.Assert(err, jc.ErrorIsNil)
	defer func() {
		err := w.Stop()
		c.Assert(err, jc.ErrorIsNil)
	}()

	deltasC := make(chan []multiwatcher.Delta)
	go func() {
		deltas, err := w.Next()
		c.Assert(err, jc.ErrorIsNil)
		deltasC <- deltas
	}()

	select {
	case deltas := <-deltasC:
		c.Assert(deltas, gc.HasLen, 1)
		modelInfo := deltas[0].Entity.(*multiwatcher.ModelInfo)

		env, err := s.State.Environment()
		c.Assert(err, jc.ErrorIsNil)

		c.Assert(modelInfo.ModelUUID, gc.Equals, env.UUID())
		c.Assert(modelInfo.Name, gc.Equals, env.Name())
		c.Assert(modelInfo.Life, gc.Equals, multiwatcher.Life("alive"))
		c.Assert(modelInfo.Owner, gc.Equals, env.Owner().Id())
		c.Assert(modelInfo.ServerUUID, gc.Equals, env.ControllerUUID())
	case <-time.After(testing.LongWait):
		c.Fatal("timed out")
	}
}

func (s *controllerSuite) TestModelStatus(c *gc.C) {
	controller := s.OpenAPI(c)
	envTag := s.State.EnvironTag()
	results, err := controller.ModelStatus(envTag)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results, jc.DeepEquals, []base.ModelStatus{{
		UUID:               envTag.Id(),
		HostedMachineCount: 0,
		ServiceCount:       0,
		Owner:              "dummy-admin@local",
		Life:               params.Alive,
	}})
}