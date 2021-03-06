package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

type TagName struct {
	Index int
	Bson  string
	Json  string
}

type MapBatchHandler struct {
	LogError     func(context.Context, string)
	LogInfo      func(context.Context, string)
	modelType    reflect.Type
	modelsType   reflect.Type
	Write        func(ctx context.Context, models []map[string]interface{}) ([]int, []int, error) // Return: Success indices, Fail indices, Error
	mapJsonIndex map[string]TagName
}

func NewMapBatchHandler(modelType reflect.Type, writeBatch func(context.Context, []map[string]interface{}) ([]int, []int, error), logs ...func(context.Context, string)) *MapBatchHandler {
	modelsType := reflect.Zero(reflect.SliceOf(modelType)).Type()
	typesTag := []string{"json", "bson"}
	mapJsonIndex := BuildMapField(modelType, typesTag, "json")
	h := &MapBatchHandler{modelType: modelType, modelsType: modelsType, Write: writeBatch, mapJsonIndex: mapJsonIndex}
	if len(logs) >= 1 {
		h.LogError = logs[0]
	}
	if len(logs) >= 2 {
		h.LogInfo = logs[1]
	}
	return h
}

func (h *MapBatchHandler) Handle(ctx context.Context, data []*Message) ([]*Message, error) {
	failMessages := make([]*Message, 0)

	var v = reflect.Indirect(reflect.ValueOf(h.initModels()))
	var messagesByteData = make([][]byte, 0)
	for _, message := range data {
		if message.Data != nil {
			messagesByteData = append(messagesByteData, message.Data)
		}
	}
	if h.LogInfo != nil {
		sv, er0 := json.Marshal(v.Interface())
		if er0 != nil {
			h.LogInfo(ctx, fmt.Sprintf(`models: %s`, v))
		} else {
			h.LogInfo(ctx, fmt.Sprintf(`models: %s`, sv))
		}
	}
	modelMaps, er1 := h.ConvertToMaps(messagesByteData)
	if er1 != nil {
		if h.LogError != nil {
			h.LogError(ctx, "error when converting to map: "+er1.Error())
		}
	}
	successIndices, failIndices, er2 := h.Write(ctx, modelMaps)
	if h.LogInfo != nil {
		h.LogInfo(ctx, fmt.Sprintf(`success indices %v fail indices %v`, successIndices, failIndices))
	}
	if er2 != nil {
		if h.LogError != nil {
			h.LogError(ctx, fmt.Sprintf("Cannot write batch: %s  Error: %s", v.Interface(), er2.Error()))
		}
		return data, er2
	}
	for _, failIndex := range failIndices {
		failMessages = append(failMessages, data[failIndex])
	}

	return failMessages, nil
}

func (h *MapBatchHandler) ConvertToMaps(v interface{}) ([]map[string]interface{}, error) {
	modelMaps := make([]map[string]interface{}, 0)
	switch reflect.TypeOf(v).Kind() {
	case reflect.Slice:
		models := reflect.Indirect(reflect.ValueOf(v))
		for i := 0; i < models.Len(); i++ {
			model, errToMap := h.StructToMap(models.Index(i).Interface())
			if errToMap == nil {
				modelMaps = append(modelMaps, model)
			}
		}
	}
	return modelMaps, nil
}

func (h *MapBatchHandler) StructToMap(bytes interface{}) (map[string]interface{}, error) {
	maps := make(map[string]interface{})
	if bytes != nil {
		b, ok := bytes.([]byte)
		if ok {
			s := string(b)
			body := make(map[string]interface{})
			er1 := json.NewDecoder(strings.NewReader(s)).Decode(&body)
			if er1 != nil {
				return maps, er1
			}
			bodyStruct := reflect.New(h.modelType).Interface()
			err2 := json.NewDecoder(strings.NewReader(s)).Decode(&bodyStruct)
			if err2 != nil {
				return maps, err2
			}
			for keyJsonName, _ := range body {
				if tag, ok := h.mapJsonIndex[keyJsonName]; ok {
					if tag.Index >= 0 {
						v, _, errv := GetValue(bodyStruct, tag.Index)
						if errv == nil {
							if tag.Bson != "" {
								maps[tag.Bson] = v
							} else {
								maps[tag.Json] = v
							}
						}
					}
				}
			}
			return maps, nil
		} else {
			return maps, fmt.Errorf("must is byte")
		}
	}
	return maps, fmt.Errorf("ERROR StructToMap with value Nil")
}

func GetValue(model interface{}, index int) (interface{}, string, error) {
	valueObject := reflect.Indirect(reflect.ValueOf(model))
	return valueObject.Field(index).Interface(), valueObject.Type().Field(index).Name, nil
}

func BuildMapField(modelType reflect.Type, tagTypes []string, tagType string) map[string]TagName {
	model := reflect.New(modelType).Interface()
	val := reflect.Indirect(reflect.ValueOf(model))
	m := make(map[string]TagName)
	for i := 0; i < val.Type().NumField(); i++ {
		field := val.Type().Field(i)
		tagName := TagName{Index: i}
		keyTag := ""
		for _, tagItem := range tagTypes {
			tagStr, ok := field.Tag.Lookup(tagItem)
			if ok {
				keyOfTagNameItem := strings.Split(tagStr, ",")[0]
				if tagItem == "bson" {
					tagName.Bson = keyOfTagNameItem
				} else if tagItem == "json" {
					tagName.Json = keyOfTagNameItem
				}
				if tagItem == tagType {
					keyTag = keyOfTagNameItem
				}
			}
		}
		if keyTag != "" {
			m[keyTag] = tagName
		} else {
			m[field.Name] = tagName
		}
	}
	return m
}

func (h *MapBatchHandler) initModels() interface{} {
	return reflect.New(h.modelsType).Interface()
}
