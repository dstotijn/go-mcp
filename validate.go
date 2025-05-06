// Copyright 2025 David Stotijn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mcp

import "github.com/dstotijn/valtor"

var initializeParamsValSchema = valtor.Object[InitializeParams]().Map(valtor.FieldValidatorMap[InitializeParams]{
	"protocolVersion": func(i InitializeParams) error {
		return valtor.String().Required().Validate(i.ProtocolVersion)
	},
	"clientInfo": func(i InitializeParams) error {
		return implValSchema.Validate(i.ClientInfo)
	},
})

var implValSchema = valtor.Object[Implementation]().Map(valtor.FieldValidatorMap[Implementation]{
	"name": func(i Implementation) error {
		return valtor.String().Required().Validate(i.Name)
	},
	"version": func(i Implementation) error {
		return valtor.String().Required().Validate(i.Version)
	},
})

// Validate checks if the InitializeParams object is valid according to the related schema.
func (ip InitializeParams) Validate() error {
	return initializeParamsValSchema.Validate(ip)
}

// Validate checks if the Implementation object is valid according to the related schema.
func (i Implementation) Validate() error {
	return implValSchema.Validate(i)
}

var getPromptParamsValSchema = valtor.Object[GetPromptParams]().Map(valtor.FieldValidatorMap[GetPromptParams]{
	"name": func(i GetPromptParams) error {
		return valtor.String().Required().Validate(i.Name)
	},
})

var toolValSchema = valtor.Object[Tool]().Map(valtor.FieldValidatorMap[Tool]{
	"name": func(i Tool) error {
		return valtor.String().Required().Validate(i.Name)
	},
})

// Validate checks if the Tool object is valid according to the related schema.
func (t Tool) Validate() error {
	return toolValSchema.Validate(t)
}

var readResourceParamsValSchema = valtor.Object[ReadResourceParams]().Map(valtor.FieldValidatorMap[ReadResourceParams]{
	"uri": func(i ReadResourceParams) error {
		return valtor.String().Required().Validate(i.URI)
	},
})

var completeArgumentValSchema = valtor.Object[CompleteArgument]().Map(valtor.FieldValidatorMap[CompleteArgument]{
	"name": func(i CompleteArgument) error {
		return valtor.String().Required().Validate(i.Name)
	},
})

var completeParamsValSchema = valtor.Object[CompleteParams]().Map(valtor.FieldValidatorMap[CompleteParams]{
	"argument": func(i CompleteParams) error {
		return completeArgumentValSchema.Validate(i.Argument)
	},
	"ref": func(i CompleteParams) error {
		// TODO: Implement
		return nil
	},
})

// Validate checks if the CompleteParams object is valid according to the related schema.
func (cp CompleteParams) Validate() error {
	return completeParamsValSchema.Validate(cp)
}
