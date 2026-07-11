package configurator

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/spf13/cast"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	DEFAULT_MAPKEY = "<mapkey>"
)

type sFlag struct {
	Key     string
	Short   string
	Desc    string
	Type    reflect.Type
	Value   reflect.Value
	Default string
	Mapkey  string
}

type Flagger struct {
	viper  *viper.Viper
	flags  *pflag.FlagSet
	mapkey string
}

func New() *Flagger {
	return &Flagger{}
}

func (f *Flagger) GetMapkey() string {
	if f.mapkey == "" {
		f.mapkey = DEFAULT_MAPKEY
	}
	if !strings.Contains(f.mapkey, "<") {
		f.mapkey = "<" + f.mapkey
	}
	if !strings.Contains(f.mapkey, ">") {
		f.mapkey = f.mapkey + ">"
	}
	return f.mapkey
}

func (f *Flagger) UseMapKey(mapkey string) {
	f.mapkey = mapkey
}

func (f *Flagger) UseViper(v *viper.Viper) {
	f.viper = v
}

func (f *Flagger) GetViper() *viper.Viper {
	if f.viper == nil {
		f.viper = viper.New()
		f.viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	}
	return f.viper
}

func (f *Flagger) UseFlags(fs *pflag.FlagSet) {
	f.flags = fs
}

func (f *Flagger) GetFlags() *pflag.FlagSet {
	if f.flags == nil {
		f.flags = pflag.NewFlagSet(os.Args[0], pflag.ContinueOnError)
	}
	return f.flags
}

func (f *Flagger) UseConfigFileArgDefault() {
	f.UseConfigFileArg("config", "c", "")
}

func (f *Flagger) UseConfigFileArg(argNameLong, argNameShort, defaultFileName string) {
	var file string
	f.GetFlags().StringVarP(&file, argNameLong, argNameShort, defaultFileName, "config file path")
	if file != "" {
		f.GetViper().SetConfigFile(file)
	}
}

func (f *Flagger) UseConfigFileName(name string) {
	f.GetViper().SetConfigName(name)
}

func (f *Flagger) UseConfigTypeYaml() {
	f.UseConfigType(`yaml`)
}

func (f *Flagger) UseConfigType(configType string) {
	f.GetViper().SetConfigType(configType)
}

func (f *Flagger) UseConfigPathDefault() {
	f.UseConfigPath("./", "./config/")
}

func (f *Flagger) UseConfigPath(dirs ...string) {
	for _, dir := range dirs {
		f.GetViper().AddConfigPath(dir)
	}
}

func (f *Flagger) UseEnvPrefix(prefix string) {
	f.GetViper().SetEnvPrefix(prefix)
}

func (f *Flagger) UseEnvKeyReplacer(replacer *strings.Replacer) {
	f.GetViper().SetEnvKeyReplacer(replacer)
}

func (f *Flagger) BindEnv(in ...string) {
	f.GetViper().BindEnv(in...)
}

func (f *Flagger) Parse(o interface{}, args ...string) error {

	if len(args) == 0 {
		args = os.Args[1:]
	}

	env := os.Environ()

	flags := f.GetFlags()
	vip := f.GetViper()

	m, err := parseFlagsMap(o, f.GetMapkey())
	if err != nil {
		return err
	}
	for k, v := range m {
		bindFlags(flags, k, v)
		vip.BindEnv(k)
	}
	for _, arg := range args {
		maparg := strings.TrimLeft(arg, "-")
		maparg = strings.SplitN(maparg, "=", 2)[0]
		mapkey := findMapKey(maparg, m, f.GetMapkey())
		if mapkey == nil {
			continue
		}
		bindFlags(flags, maparg, *mapkey)
		vip.BindEnv(maparg)
	}

	prefix := vip.GetEnvPrefix()
	if prefix != "" {
		prefix = prefix + "_"
	}
	for _, env := range env {
		if prefix != "" && !strings.HasPrefix(env, prefix) {
			continue
		}
		mapenv := strings.SplitN(env, "=", 2)[0]
		mapenv = strings.TrimLeft(mapenv, prefix)
		mapkey := findMapKey(mapenv, m, f.GetMapkey())
		if mapkey == nil {
			continue
		}
		bindFlags(flags, mapenv, *mapkey)
		vip.BindEnv(mapenv)
	}

	flags.Parse(args)

	vip.AddConfigPath("./")

	cfg := vip.ConfigFileUsed()
	err = vip.ReadInConfig()
	if err != nil && cfg != "" {
		return err
	}

	vip.AutomaticEnv()

	vip.BindPFlags(flags)

	return f.unmarshal(o, m)
}

func (f *Flagger) unmarshal(o interface{}, flags map[string]sFlag) error {
	mapkey := f.GetMapkey()
	hook := mapstructure.ComposeDecodeHookFunc(
		func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
			if f.Kind() != reflect.Map {
				return data, nil
			}
			if data, ok := data.(map[string]interface{}); ok {
				for key := range data {
					if strings.Contains(key, mapkey) {
						delete(data, key)
					}
					for _, v := range flags {
						if strings.Contains(key, v.Mapkey) {
							delete(data, key)
						}
					}
				}
			}
			if t.Kind() != reflect.Struct {
				return data, nil
			}

			for i := 0; i < t.NumField(); i++ {
				field := t.Field(i)
				mapstructureTag := field.Tag.Get("mapstructure")
				if mapstructureTag == "" {
					continue
				}
				defaultTag := field.Tag.Get("default")
				if defaultTag == "" {
					continue
				}
				if data, ok := data.(map[string]interface{}); ok {
					if _, ok := data[mapstructureTag]; ok {
						continue
					}
					data[mapstructureTag] = defaultTag
				}
			}
			return data, nil
		},
	)
	return f.GetViper().Unmarshal(o, viper.DecodeHook(hook))
}

func parseFlagsMap(c interface{}, mapkey string) (map[string]sFlag, error) {
	m := make(map[string]sFlag)
	err := parseFlags(&m, c, "", nil, nil, mapkey)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func parseFlags(m *map[string]sFlag, c interface{}, parent string, field *reflect.StructField, fieldValue *reflect.Value, mapkey string) error {
	var name string
	if field != nil {
		mapstructure := field.Tag.Get("mapstructure")
		if mapstructure != "" {
			name = mapstructure
		} else {
			name = strings.ToLower(field.Name)
		}
	}
	var key string
	if parent != "" && name != "" {
		key = parent + "." + name
	} else {
		key = parent + name
	}

	t := reflect.TypeOf(c)
	v := reflect.ValueOf(c)

	if t.Kind() == reflect.Ptr {
		t = t.Elem()
		v = v.Elem()
	}

	switch t.Kind() {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.String,
		reflect.Array, reflect.Slice:
		if strings.EqualFold(field.Tag.Get("hidden"), "true") {
			return nil
		}
		(*m)[key] = sFlag{
			Key:     key,
			Default: field.Tag.Get("default"),
			Short:   field.Tag.Get("short"),
			Desc:    field.Tag.Get("description"),
			Type:    field.Type,
			Value:   *fieldValue,
			Mapkey:  mapkey,
		}
		return nil
	case reflect.Map:
		valueType := t.Elem()
		valueNew := reflect.New(valueType)
		if valueNew.Type().Kind() == reflect.Ptr {
			valueNew = valueNew.Elem()
		}
		tagmap := field.Tag.Get("mapkey")
		if tagmap != "" {
			mapkey = tagmap
		}
		err := parseFlags(m, valueNew.Interface(), key+"."+mapkey, nil, nil, mapkey)
		if err != nil {
			return err
		}
		return nil
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			fv := v.Field(i)
			if !f.IsExported() {
				continue
			}
			if !fv.IsValid() {
				return fmt.Errorf("cannot parse unexported field : %+v", f)
			}
			err := parseFlags(m, fv.Interface(), key, &f, &fv, mapkey)
			if err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("parsing flag error: unsupport type : %+v", t.Kind())
	}
}

func bindFlags(flags *pflag.FlagSet, k string, v sFlag) error {
	switch v.Type.Kind() {
	case reflect.Bool:
		if !v.Value.IsZero() || v.Default == "" {
			flags.BoolP(k, v.Short, v.Value.Bool(), v.Desc)
		} else {
			flags.BoolP(k, v.Short, cast.ToBool(v.Default), v.Desc)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if !v.Value.IsZero() || v.Default == "" {
			flags.Int64P(k, v.Short, v.Value.Int(), v.Desc)
		} else {
			flags.Int64P(k, v.Short, cast.ToInt64(v.Default), v.Desc)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if !v.Value.IsZero() || v.Default == "" {
			flags.Uint64P(k, v.Short, v.Value.Uint(), v.Desc)
		} else {
			flags.Uint64P(k, v.Short, cast.ToUint64(v.Default), v.Desc)
		}
	case reflect.Float32, reflect.Float64:
		if !v.Value.IsZero() || v.Default == "" {
			flags.Float64P(k, v.Short, v.Value.Float(), v.Desc)
		} else {
			flags.Float64P(k, v.Short, cast.ToFloat64(v.Default), v.Desc)
		}
	case reflect.String:
		if v.Value.String() != "" {
			flags.StringP(k, v.Short, v.Value.String(), v.Desc)
		} else {
			flags.StringP(k, v.Short, v.Default, v.Desc)
		}
	case reflect.Slice:
		et := v.Type.Elem()
		switch et.Kind() {
		case reflect.Bool:
			flags.BoolSlice(k, []bool{}, v.Desc)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			flags.Int64Slice(k, []int64{}, v.Desc)
		case reflect.Float32, reflect.Float64:
			flags.Float64Slice(k, []float64{}, v.Desc)
		case reflect.String:
			flags.StringSlice(k, []string{}, v.Desc)
		default:
			return fmt.Errorf("set cmd flag error: unsupport slice type : %+v", et.Kind())
		}
	default:
		return fmt.Errorf("set cmd flag error: unsupport type : %+v", v.Type.Kind())
	}
	return nil
}

func findMapKey(arg string, m map[string]sFlag, mapkey string) *sFlag {
	for k, v := range m {
		if isMapKey(k, arg, mapkey) {
			return &v
		}
		if isMapKey(k, arg, v.Mapkey) {
			return &v
		}
	}
	return nil
}

func isMapKey(l, r, mapkey string) bool {
	if !strings.Contains(l, mapkey) && !strings.Contains(r, mapkey) {
		return false
	}
	ls := strings.Split(l, ".")
	rs := strings.Split(r, ".")
	if len(ls) != len(rs) {
		return false
	}
	for i := range ls {
		if ls[i] == "" || rs[i] == "" {
			return false
		}
		if ls[i] == mapkey || rs[i] == mapkey {
			continue
		}
		if ls[i] != rs[i] {
			return false
		}
	}
	return true
}
