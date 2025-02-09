/*
 * Copyright (c) 2017, MegaEase
 * All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package worker

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"runtime/debug"
	"sync"

	"github.com/megaease/easegress/pkg/logger"

	"github.com/kataras/iris"
	iriscontext "github.com/kataras/iris/context"
	"gopkg.in/yaml.v2"
)

const (
	defaultServerIP = "127.0.0.1"
)

type (
	apiServer struct {
		app       *iris.Application
		apisMutex sync.RWMutex
		apis      []*apiEntry
		port      int
	}

	apiEntry struct {
		Path    string       `yaml:"path"`
		Method  string       `yaml:"method"`
		Handler iris.Handler `yaml:"-"`
	}

	apiErr struct {
		Code    int    `yaml:"code"`
		Message string `yaml:"message"`
	}
)

// NewAPIServer creates a initialed API server.
func NewAPIServer(port int) *apiServer {
	app := iris.New()

	s := &apiServer{
		app:  app,
		port: port,
	}

	// NOTE: Fix trailing slash problem.
	// Reference: https://github.com/kataras/iris/issues/820#issuecomment-383131098
	app.WrapRouter(func(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
		path := r.URL.Path
		if len(path) > 1 && path[len(path)-1] == '/' && path[len(path)-2] != '/' {
			path = path[:len(path)-1]
			r.RequestURI = path
			r.URL.Path = path
		}
		next(w, r)
	})

	app.Use(newRecoverer())
	app.Logger().SetOutput(ioutil.Discard)
	s.addListAPI()

	return s
}

// Run calls iris app for servering RESTful APIs.
func (s *apiServer) run() {
	addr := fmt.Sprintf("%s:%d", defaultServerIP, s.port)
	logger.Infof("worker api server running in %s", addr)

	err := s.app.Run(iris.Addr(addr))
	if err == iris.ErrServerClosed {
		return
	}
	if err != nil {
		logger.Errorf("run worker api app failed: %v", err)
		os.Exit(1)
	}
}

func (s *apiServer) addListAPI() {
	listAPIs := []*apiEntry{
		{
			Path:    "/",
			Method:  "GET",
			Handler: s.listAPIs,
		},
	}

	s.registerAPIs(listAPIs)
}

func (s *apiServer) listAPIs(ctx iriscontext.Context) {
	s.apisMutex.RLock()
	defer s.apisMutex.RUnlock()

	buff, err := yaml.Marshal(s.apis)
	if err != nil {
		panic(fmt.Errorf("marshal %#v to yaml failed: %v", s.apis, err))
	}

	ctx.Header("Content-Type", "text/vnd.yaml")
	ctx.Write(buff)
}

func (s *apiServer) Close() {
	s.app.Shutdown(context.Background())
}

func (s *apiServer) registerAPIs(apis []*apiEntry) {
	s.apisMutex.Lock()
	defer s.apisMutex.Unlock()

	s.apis = append(s.apis, apis...)

	for _, api := range apis {
		logger.Infof("api method: %s, path: %s, handler %#v", api.Method, api.Path, api.Handler)
		switch api.Method {
		case "GET":
			s.app.Get(api.Path, api.Handler)
		case "HEAD":
			s.app.Head(api.Path, api.Handler)
		case "PUT":
			s.app.Put(api.Path, api.Handler)
		case "POST":
			s.app.Post(api.Path, api.Handler)
		case "PATCH":
			s.app.Patch(api.Path, api.Handler)
		case "DELETE":
			s.app.Delete(api.Path, api.Handler)
		case "CONNECT":
			s.app.Connect(api.Path, api.Handler)
		case "OPTIONS":
			s.app.Options(api.Path, api.Handler)
		case "TRACE":
			s.app.Trace(api.Path, api.Handler)
		}
	}

	s.app.RefreshRouter()
}

func handleAPIError(ctx iris.Context, code int, err error) {
	ctx.StatusCode(code)
	buff, err := yaml.Marshal(apiErr{
		Code:    code,
		Message: err.Error(),
	})
	if err != nil {
		panic(err)
	}
	ctx.Write(buff)
}

func newRecoverer() func(iriscontext.Context) {
	return func(ctx iriscontext.Context) {
		defer func() {
			if err := recover(); err != nil {
				if ctx.IsStopped() {
					return
				}

				logger.Errorf("recover from %s, err: %v, stack trace:\n%s\n",
					ctx.HandlerName(), err, debug.Stack())
				handleAPIError(ctx, http.StatusInternalServerError, fmt.Errorf("%v", err))
			}
		}()

		ctx.Next()
	}
}
