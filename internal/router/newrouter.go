package router

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"
)

type (
	ResolverConfFunc func(router *Router, varName string)
	ResolverFunc     func(w http.ResponseWriter, r *http.Request) (any, error)
	GlobalConfFunc   func(router *Router)
)

type Router struct {
	fs        fs.FS
	resolvers map[string]map[string]ResolverFunc
	globals   map[string]func() any
	templates *template.Template
}

type PathVariables map[string]string

var PathVariablesKey = &PathVariables{}

func (router *Router) UseGlobals(configFunc ...GlobalConfFunc) {
	for _, f := range configFunc {
		f(router)
	}
}

func (router *Router) UseResolver(varName string, configFunc ResolverConfFunc) {
	configFunc(router, varName)
}

func Global(name string, globalFunc func() any) GlobalConfFunc {
	return func(router *Router) {
		router.globals[name] = globalFunc
	}
}

func Get(resolverFunc ResolverFunc) ResolverConfFunc {
	return MethodHandler(http.MethodGet, resolverFunc)
}

func Post(resolverFunc ResolverFunc) ResolverConfFunc {
	return MethodHandler(http.MethodPost, resolverFunc)
}

func Put(resoverFunc ResolverFunc) ResolverConfFunc {
	return MethodHandler(http.MethodPut, resoverFunc)
}

func Patch(resolverFunc ResolverFunc) ResolverConfFunc {
	return MethodHandler(http.MethodPatch, resolverFunc)
}

func Delete(resoverFunc ResolverFunc) ResolverConfFunc {
	return MethodHandler(http.MethodDelete, resoverFunc)
}

func MethodHandler(m string, handler ResolverFunc) ResolverConfFunc {
	return func(router *Router, varName string) {
		variableResolvers := router.resolvers[varName]
		if variableResolvers == nil {
			router.resolvers[varName] = make(map[string]ResolverFunc)
		}
		router.resolvers[varName][m] = func(w http.ResponseWriter, r *http.Request) (any, error) {
			return handler(w, r)
		}
	}
}

func NewRouter(fs fs.FS) *Router {
	return &Router{
		fs:        fs,
		resolvers: make(map[string]map[string]ResolverFunc),
		globals:   make(map[string]func() any),
	}
}

type Render struct {
	layout   string
	template string
	data     map[string]any
}

func (router *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	router.templates = scanTemplates(router.fs)

	render, err := router.getRender(w, r)

	globals := make(map[string]any)
	for name, f := range router.globals {
		globals[name] = f()
	}
	render.data["globals"] = globals

	if err != nil {
		daveError := &daveError{}
		if errors.As(err, daveError) {
			t := router.templates.Lookup(daveError.fallback)
			if t != nil {
				render.data["error"] = daveError.cause
				router.templates.ExecuteTemplate(w, daveError.fallback, render.data)
			} else {
				w.Write([]byte(fmt.Sprintf("%s: %s", daveError.message, daveError.cause)))
			}
		}
		// lookup custom fallback
		return
	}
	log.Println(render)
	if render.layout == "" {
		err = router.templates.ExecuteTemplate(w, render.template, render.data)
		return
	}
	content := &strings.Builder{}
	err = router.templates.ExecuteTemplate(content, render.template, render.data)
	err = router.templates.ExecuteTemplate(w, render.layout, map[string]string{"content": content.String()})
}

func scanTemplates(root fs.FS) *template.Template {
	rootTemplate := template.New("root")
	fs.WalkDir(root, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Panic(err)
		}
		if path == "." {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		newTemplate := rootTemplate.New(stripTemplateSuffix(path))
		file, err := root.Open(path)
		if err != nil {
			log.Panic(err)
		}
		defer file.Close()
		content, err := io.ReadAll(file)
		if err != nil {
			log.Panic(err)
		}
		_, err = newTemplate.Parse(string(content))
		if err != nil {
			log.Panic(err)
		}
		return nil
	})
	return rootTemplate
}

func (router *Router) getRender(w http.ResponseWriter, r *http.Request) (*Render, error) {
	render := &Render{}
	templateName := r.Header.Get("D-TEMPLATE")
	if templateName == "" {
		templateName = "index"
	}
	reqPath := strings.Join([]string{r.URL.Path, templateName}, "/")

	data := make(map[string]any)

	templatePath, pathVariables := parseRequestPath(router.templates, reqPath)
	data["path_variables"] = pathVariables
	if templatePath == "" {
		w.WriteHeader(http.StatusNotFound)
		return nil, NotFound(fmt.Errorf("no template at %s", reqPath))
	}

	resolverCtx := context.WithValue(r.Context(), PathVariablesKey, pathVariables)
	resolverReq := r.WithContext(resolverCtx)

	keys := make([]string, 0, len(pathVariables))
	for k := range pathVariables {
		keys = append(keys, k)
	}

	for name := range pathVariables {
		resolvers := router.resolvers[name]
		if resolvers == nil {
			continue
		}
		var resolver ResolverFunc
		if name == keys[len(keys)-1] {
			resolver = resolvers[r.Method]
		} else {
			resolver = resolvers[http.MethodGet]
		}
		if resolver == nil {
			continue
		}

		d, err := resolver(w, resolverReq)
		if err != nil {
			return &Render{
				data: data,
			}, err
		}
		data[name] = d
	}
	render.data = data
	render.template = templatePath

	layoutPath := r.Header.Get("D-LAYOUT")
	if layoutPath == "" {
		layoutPath = "default"
	}
	layoutPath = strings.Join([]string{"layouts", layoutPath}, "/")
	layoutTemplate := router.templates.Lookup(layoutPath)
	if layoutTemplate == nil {
		log.Printf("layout not found: %s, rendering w/o layout", layoutPath)
		return render, nil
	}

	render.layout = layoutPath
	return render, nil
}

func parseRequestPath(templates *template.Template, path string) (string, PathVariables) {
	reqSegments := strings.Split(path[1:], "/")
	templatePath := ""
	pathVariables := PathVariables{}

	for _, v := range templates.Templates() {
		if v.Name() == "root" {
			continue
		}
		path := stripTemplateSuffix(v.Name())
		pathSegments := strings.Split(path, "/")
		if len(pathSegments) != len(reqSegments) {
			continue
		}
		found := true
		for i, seg := range pathSegments {
			if seg == reqSegments[i] {
				continue
			} else {
				if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
					varName := seg[1 : len(seg)-1]
					pathVariables[varName] = reqSegments[i]
				} else {
					found = false
					break
				}
			}
		}
		if found {
			templatePath = path
		} else {
			pathVariables = PathVariables{}
		}
	}
	return templatePath, pathVariables
}

func stripTemplateSuffix(t string) string {
	i := strings.LastIndex(t, ".tmpl")
	if i < 0 {
		return t
	}
	return t[:i]
}

func VariableValue(r *http.Request, varName string) any {
	pathVariables := r.Context().Value(PathVariablesKey).(PathVariables)
	return pathVariables[varName]
}

type daveError struct {
	message  string
	fallback string
	cause    error
}

func (daveError daveError) Error() string {
	return daveError.message
}

func NotFound(cause error) error {
	return daveError{
		message:  "not found",
		fallback: "fallback/not_found",
		cause:    cause,
	}
}

func Unexpected(cause error) error {
	return daveError{
		message:  "unexpected error",
		fallback: "fallback/unexpected_error",
		cause:    cause,
	}
}
