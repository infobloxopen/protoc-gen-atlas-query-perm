package plugin

import (
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"github.com/gogo/protobuf/protoc-gen-gogo/generator"
	plugin "github.com/gogo/protobuf/protoc-gen-gogo/plugin"

	"github.com/infobloxopen/protoc-gen-atlas-query-validate/options"
)

const (
	filtering                          = ".infoblox.api.Filtering"
	sorting                            = ".infoblox.api.Sorting"
	fieldSelection                     = ".infoblox.api.FieldSelection"
	messagesValidationVarSuffix        = "MessagesRequireQueryValidation"
	methodFilteringVarSuffix           = "MethodsRequireFilteringValidation"
	methodSortingVarSuffix             = "MethodsRequireSortingValidation"
	methodFieldSelectionVarSuffix      = "MethodsRequireFieldSelectionValidation"
	validateFilteringMethodSuffix      = "ValidateFiltering"
	validateSortingMethodSuffix        = "ValidateSorting"
	validateFieldSelectionMethodSuffix = "ValidateFieldSelection"

	protoTypeTimestamp   = ".google.protobuf.Timestamp"
	protoTypeUUID        = ".gorm.types.UUID"
	protoTypeUUIDValue   = ".gorm.types.UUIDValue"
	protoTypeResource    = ".atlas.rpc.Identifier"
	protoTypeInet        = ".gorm.types.InetValue"
	protoTypeJSONValue   = ".gorm.types.JSONValue"
	protoTypeStringValue = ".google.protobuf.StringValue"
	protoTypeDoubleValue = ".google.protobuf.DoubleValue"
	protoTypeFloatValue  = ".google.protobuf.FloatValue"
	protoTypeInt32Value  = ".google.protobuf.Int32Value"
	protoTypeInt64Value  = ".google.protobuf.Int64Value"
	protoTypeUInt32Value = ".google.protobuf.UInt32Value"
	protoTypeUInt64Value = ".google.protobuf.UInt64Value"
	protoTypeBoolValue   = ".google.protobuf.BoolValue"
)

// QueryValidatePlugin implements the plugin interface and creates validations for collection operation parameters code from .protos
type QueryValidatePlugin struct {
	*generator.Generator
	currentFile                             *generator.FileDescriptor
	messagesValidationVarName               string
	requiredFilteringValidationVarName      string
	requiredSortingValidationVarName        string
	validateFilteringMethodName             string
	validateSortingMethodName               string
	validateFieldSelectionMethodName        string
	requiredFieldSelectionValidationVarName string
	maxNesting                              int
	alwaysNest                              bool
}

func (p *QueryValidatePlugin) setFile(file *generator.FileDescriptor) {
	p.currentFile = file
	// p.Generator.SetFile(file.FileDescriptorProto)

	baseFileName := filepath.Base(file.GetName())
	p.messagesValidationVarName = generator.CamelCase(strings.TrimSuffix(baseFileName, filepath.Ext(baseFileName)) + messagesValidationVarSuffix)
	p.requiredFilteringValidationVarName = generator.CamelCase(strings.TrimSuffix(baseFileName, filepath.Ext(baseFileName)) + methodFilteringVarSuffix)
	p.requiredSortingValidationVarName = generator.CamelCase(strings.TrimSuffix(baseFileName, filepath.Ext(baseFileName)) + methodSortingVarSuffix)
	p.requiredFieldSelectionValidationVarName = generator.CamelCase(strings.TrimSuffix(baseFileName, filepath.Ext(baseFileName)) + methodFieldSelectionVarSuffix)
	p.validateFilteringMethodName = generator.CamelCase(strings.TrimSuffix(baseFileName, filepath.Ext(baseFileName)) + validateFilteringMethodSuffix)
	p.validateSortingMethodName = generator.CamelCase(strings.TrimSuffix(baseFileName, filepath.Ext(baseFileName)) + validateSortingMethodSuffix)
	p.validateFieldSelectionMethodName = generator.CamelCase(strings.TrimSuffix(baseFileName, filepath.Ext(baseFileName)) + validateFieldSelectionMethodSuffix)
}

// Name identifies the plugin
func (p *QueryValidatePlugin) Name() string {
	return "atlas-query-validate"
}

// Init is called once after data structures are built but before
// code generation begins.
func (p *QueryValidatePlugin) Init(g *generator.Generator) {
	p.Generator = g
	if v, ok := g.Param["nested_field_depth_limit"]; ok {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			log.Print("Invalid parameter for nest_depth, should be an integer")
		} else {
			p.maxNesting = int(n)
		}
	} else {
		p.maxNesting = 2
	}
	if p.maxNesting < 1 {
		p.maxNesting = 1
	}
	if v, ok := g.Param["enable_nested_fields"]; ok {
		p.alwaysNest, _ = strconv.ParseBool(v)
	} else {
		p.alwaysNest = false
	}
}

// Generate produces the code generated by the plugin for this file,
// except for the imports, by calling the generator's methods P, In, and Out.
func (p *QueryValidatePlugin) Generate(file *generator.FileDescriptor) {
	p.setFile(file)
	p.genValidationData()
	p.genValidateFiltering()
	p.genValidateSorting()
	p.genValidateFieldSelection()
}

func (p *QueryValidatePlugin) genValidationData() {
	p.genFiltering()
	p.genSorting()
	p.genFieldSelection()
}

func (p *QueryValidatePlugin) genFiltering() {
	p.P(`var `, p.requiredFilteringValidationVarName, ` = map[string]map[string]options.FilteringOption {`)
	for _, srv := range p.currentFile.GetService() {
		for _, method := range srv.GetMethod() {
			hasFiltering := p.hasFiltering(p.ObjectNamed(method.GetInputType()).(*generator.Descriptor))
			outputMsg := p.ObjectNamed(method.GetOutputType()).(*generator.Descriptor)
			resultMsg := p.getResultMessage(outputMsg)
			if hasFiltering && resultMsg != nil {
				p.P(`"`, fmt.Sprintf("/%s.%s/%s", p.currentFile.GetPackage(), srv.GetName(), method.GetName()), `": map[string]options.FilteringOption{`)
				filteringInfo := p.getFilteringData(resultMsg)
				for _, v := range filteringInfo {
					var f string
					if len(v.option.Deny) != 0 {
						for _, d := range v.option.Deny {
							f += "options.QueryValidate_" + d.String() + `,`
						}
						f = `Deny: []options.QueryValidate_FilterOperator{` + f + `},`
					}
					t := `ValueType: options.QueryValidate_` + v.option.ValueType.String()
					p.P(`"`, v.fieldName, `": options.FilteringOption{`+f+t+`},`)
				}
				p.P(`},`)
			}
		}
	}
	p.P(`}`)
}

func (p *QueryValidatePlugin) genSorting() {
	p.P(`var `, p.requiredSortingValidationVarName, ` = map[string][]string {`)
	for _, srv := range p.currentFile.GetService() {
		for _, method := range srv.GetMethod() {
			hasSorting := p.hasSorting(p.ObjectNamed(method.GetInputType()).(*generator.Descriptor))
			outputMsg := p.ObjectNamed(method.GetOutputType()).(*generator.Descriptor)
			resultMsg := p.getResultMessage(outputMsg)
			if hasSorting && resultMsg != nil {
				p.P(`"`, fmt.Sprintf("/%s.%s/%s", p.currentFile.GetPackage(), srv.GetName(), method.GetName()), `": []string {`)
				sortingInfo := p.getSortingData(resultMsg)
				for _, v := range sortingInfo {
					p.P(`"`, v, `",`)
				}
				p.P(`},`)
			}
		}
	}
	p.P(`}`)
}

func (p *QueryValidatePlugin) genFieldSelection() {
	p.P(`var `, p.requiredFieldSelectionValidationVarName, ` = map[string][]string{`)
	for _, srv := range p.currentFile.GetService() {
		for _, method := range srv.GetMethod() {
			hasFieldSelection := p.hasFieldSelection(p.ObjectNamed(method.GetInputType()).(*generator.Descriptor))
			outputMsg := p.ObjectNamed(method.GetOutputType()).(*generator.Descriptor)
			resultMsg := p.getResultMessage(outputMsg)
			if hasFieldSelection && resultMsg != nil {
				p.P(`"`, fmt.Sprintf("/%s.%s/%s", p.currentFile.GetPackage(), srv.GetName(), method.GetName()), `": {`)
				fields := p.getFieldSelectionData(resultMsg)
				for _, field := range fields {
					p.P(fmt.Sprintf(`"%s",`, field))
				}
				p.P(`},`)
			}
		}
	}
	p.P(`}`)
}

func (p *QueryValidatePlugin) hasFieldSelection(msg *generator.Descriptor) bool {
	for _, msgField := range msg.GetField() {
		if msgField.GetTypeName() == fieldSelection {
			return true
		}
	}
	return false
}

func (p *QueryValidatePlugin) hasFiltering(msg *generator.Descriptor) bool {
	for _, msgField := range msg.GetField() {
		if msgField.GetTypeName() == filtering {
			return true
		}
	}
	return false
}

func (p *QueryValidatePlugin) hasSorting(msg *generator.Descriptor) bool {
	for _, msgField := range msg.GetField() {
		if msgField.GetTypeName() == sorting {
			return true
		}
	}
	return false
}

func (p *QueryValidatePlugin) getResultMessage(msg *generator.Descriptor) *generator.Descriptor {
	for _, field := range msg.GetField() {
		switch field.GetName() {
		case "result", "results":
			if field.GetType() == descriptor.FieldDescriptorProto_TYPE_MESSAGE {
				return p.ObjectNamed(field.GetTypeName()).(*generator.Descriptor)
			}
		}
	}

	return nil
}

type fieldValidate struct {
	fieldName string
	option    options.FilteringOption
}

func (p *QueryValidatePlugin) syntheticField(name string, o *options.QueryValidate) *descriptor.FieldDescriptorProto {

	if o.GetValueTypeUrl() == "" {
		return nil
	}

	if msg := p.ObjectNamed(o.GetValueTypeUrl()); msg == nil {
		p.Fail(`Cannot find named object of type `, o.GetValueTypeUrl())
	}

	var (
		descLabel    = descriptor.FieldDescriptorProto_LABEL_OPTIONAL
		descType     = descriptor.FieldDescriptorProto_TYPE_MESSAGE
		descTypeName = o.GetValueTypeUrl()
		descOptions  = descriptor.FieldOptions{}
	)

	f := &descriptor.FieldDescriptorProto{
		Name:     &name,
		TypeName: &descTypeName,
		Type:     &descType,
		Label:    &descLabel,
		Options:  &descOptions,
	}

	if err := proto.SetExtension(f.Options, options.E_Validate, o); err != nil {
		p.Fail(`cannot set extension for field `, name, `: `, err.Error())
	}

	return f
}

func (p *QueryValidatePlugin) getFilteringData(msg *generator.Descriptor) []fieldValidate {
	return p.getFilteringDataAux(msg, p.getNestDepth(msg))
}

func (p *QueryValidatePlugin) getFilteringDataAux(msg *generator.Descriptor, maxNesting int) []fieldValidate {

	var (
		data      []fieldValidate
		fields    []*descriptor.FieldDescriptorProto
		valueType options.QueryValidate_ValueType
	)

	for _, opts := range p.getMessageQueryValidationOptions(msg.DescriptorProto) {
		if f := p.syntheticField(opts.GetName(), opts.GetValue()); f != nil {
			fields = append(fields, f)
		} else {
			data = append(data, fieldValidate{
				fieldName: opts.GetName(),
				option: options.FilteringOption{
					ValueType: opts.GetValue().GetValueType(),
					Deny:      p.getDenyRules(opts.GetName(), opts.GetValue(), opts.GetValue().GetValueType()),
				},
			})
		}
	}

	fields = append(fields, msg.GetField()...)

	for _, field := range fields {
		opts := getQueryValidationOptions(field)
		if sfield := p.syntheticField(field.GetName(), opts); sfield != nil {
			field = sfield
			opts = getQueryValidationOptions(sfield)
		}

		fieldName := field.GetName()
		if field.GetTypeName() == protoTypeJSONValue {
			fieldName += ".*"
		}

		if valueType = opts.GetValueType(); valueType == options.QueryValidate_DEFAULT {
			if field.IsRepeated() {
				data = append(data, fieldValidate{
					fieldName: fieldName,
					option: options.FilteringOption{
						ValueType: options.QueryValidate_DEFAULT,
						Deny: []options.QueryValidate_FilterOperator{
							options.QueryValidate_ALL,
						},
					},
				})
				continue
			}

			if valueType = p.getValueType(field); valueType == options.QueryValidate_DEFAULT {

				if maxNesting == 1 {
					continue
				}

				if field.GetType() == descriptor.FieldDescriptorProto_TYPE_MESSAGE && p.allowNested(msg, opts) {

					nestedMsg := p.ObjectNamed(field.GetTypeName()).(*generator.Descriptor)
					if nestedMsg == nil {
						p.Fail(`Cannot find named object of type `, field.GetTypeName())
					}

					for _, v := range p.getFilteringDataAux(nestedMsg, maxNesting-1) {
						data = append(data, fieldValidate{
							fieldName: fieldName + "." + v.fieldName,
							option:    v.option,
						})
					}

					continue
				}

				data = append(data, fieldValidate{
					fieldName: fieldName,
					option: options.FilteringOption{
						ValueType: options.QueryValidate_DEFAULT,
						Deny: []options.QueryValidate_FilterOperator{
							options.QueryValidate_ALL,
						},
					},
				})
				continue
			}
		}

		data = append(data, fieldValidate{fieldName, options.FilteringOption{ValueType: valueType, Deny: p.getDenyRules(fieldName, opts, valueType)}})
	}
	return data
}

func (p *QueryValidatePlugin) isAllowedNestedField(n string, o *options.QueryValidate) bool {
	if o == nil || len(o.NestedFields) == 0 {
		return true
	}

	for _, v := range o.NestedFields {
		if v == n {
			return true
		}
	}

	return false
}

func (p *QueryValidatePlugin) getValueType(field *descriptor.FieldDescriptorProto) options.QueryValidate_ValueType {
	switch field.GetType() {
	case descriptor.FieldDescriptorProto_TYPE_STRING:
		return options.QueryValidate_STRING
	case descriptor.FieldDescriptorProto_TYPE_ENUM:
		return options.QueryValidate_STRING
	case descriptor.FieldDescriptorProto_TYPE_BOOL:
		return options.QueryValidate_BOOL
	case descriptor.FieldDescriptorProto_TYPE_DOUBLE,
		descriptor.FieldDescriptorProto_TYPE_FLOAT,
		descriptor.FieldDescriptorProto_TYPE_INT32,
		descriptor.FieldDescriptorProto_TYPE_INT64,
		descriptor.FieldDescriptorProto_TYPE_SINT32,
		descriptor.FieldDescriptorProto_TYPE_SINT64,
		descriptor.FieldDescriptorProto_TYPE_UINT32,
		descriptor.FieldDescriptorProto_TYPE_UINT64:
		return options.QueryValidate_NUMBER
	case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
		switch field.GetTypeName() {
		case protoTypeResource,
			protoTypeTimestamp,
			protoTypeUUID,
			protoTypeUUIDValue,
			protoTypeInet,
			protoTypeStringValue,
			protoTypeJSONValue:
			return options.QueryValidate_STRING
		case protoTypeDoubleValue,
			protoTypeFloatValue,
			protoTypeInt32Value,
			protoTypeInt64Value,
			protoTypeUInt32Value,
			protoTypeUInt64Value:
			return options.QueryValidate_NUMBER
		case protoTypeBoolValue:
			return options.QueryValidate_BOOL
		default:
			return options.QueryValidate_DEFAULT
		}
	default:
		return options.QueryValidate_DEFAULT
	}
}

func (p *QueryValidatePlugin) getSortingData(msg *generator.Descriptor) []string {
	return p.getSortingDataAux(msg, p.getNestDepth(msg))
}

func (p *QueryValidatePlugin) getSortingDataAux(msg *generator.Descriptor, maxNesting int) []string {

	var (
		data      []string
		fields    []*descriptor.FieldDescriptorProto
		valueType options.QueryValidate_ValueType
	)

	for _, opts := range p.getMessageQueryValidationOptions(msg.DescriptorProto) {
		if f := p.syntheticField(opts.GetName(), opts.GetValue()); f != nil {
			fields = append(fields, f)
		} else if !opts.GetValue().GetSorting().GetDisable() {
			data = append(data, opts.GetName())
		}
	}

	fields = append(fields, msg.GetField()...)

	for _, field := range fields {
		opts := getQueryValidationOptions(field)
		if sfield := p.syntheticField(field.GetName(), opts); sfield != nil {
			field = sfield
			opts = getQueryValidationOptions(sfield)
		}

		if opts.GetSorting().GetDisable() {
			continue
		}

		fieldName := field.GetName()
		if valueType = opts.GetValueType(); valueType == options.QueryValidate_DEFAULT {

			if field.IsRepeated() {
				continue
			}

			if valueType = p.getValueType(field); valueType == options.QueryValidate_DEFAULT {

				if maxNesting == 1 {
					continue
				}

				if field.GetType() == descriptor.FieldDescriptorProto_TYPE_MESSAGE && p.allowNested(msg, opts) {

					nestedMsg := p.ObjectNamed(field.GetTypeName()).(*generator.Descriptor)
					for _, v := range p.getSortingDataAux(nestedMsg, maxNesting-1) {
						data = append(data, fieldName+"."+v)
					}
				}

				continue
			}
		}

		data = append(data, fieldName)
	}

	return data
}

func (p *QueryValidatePlugin) getFieldSelectionData(msg *generator.Descriptor) []string {
	return p.getFieldSelectionDataAux(msg, p.getNestDepth(msg))
}

func (p *QueryValidatePlugin) getFieldSelectionDataAux(msg *generator.Descriptor, maxNesting int) []string {

	var (
		data      []string
		fields    []*descriptor.FieldDescriptorProto
		valueType options.QueryValidate_ValueType
	)

	for _, opts := range p.getMessageQueryValidationOptions(msg.DescriptorProto) {
		if f := p.syntheticField(opts.GetName(), opts.GetValue()); f != nil {
			fields = append(fields, f)
		} else if !opts.GetValue().GetFieldSelection().GetDisable() {
			data = append(data, opts.GetName())
		}
	}

	fields = append(fields, msg.GetField()...)

	for _, field := range fields {
		opts := getQueryValidationOptions(field)
		if sfield := p.syntheticField(field.GetName(), opts); sfield != nil {
			field = sfield
			opts = getQueryValidationOptions(sfield)
		}

		if opts.GetFieldSelection().GetDisable() {
			continue
		}

		fieldName := field.GetName()
		if valueType = opts.GetValueType(); valueType == options.QueryValidate_DEFAULT {

			switch field.GetType() {
			case descriptor.FieldDescriptorProto_TYPE_MESSAGE:
				switch field.GetTypeName() {
				case protoTypeResource,
					protoTypeTimestamp,
					protoTypeUUID,
					protoTypeUUIDValue,
					protoTypeInet,
					protoTypeStringValue:
				case protoTypeBoolValue:
				case protoTypeDoubleValue,
					protoTypeFloatValue,
					protoTypeInt32Value,
					protoTypeInt64Value,
					protoTypeUInt32Value,
					protoTypeUInt64Value:
				default:

					if maxNesting == 1 {
						continue
					}

					nestedMsg := p.ObjectNamed(field.GetTypeName()).(*generator.Descriptor)
					for _, v := range p.getFieldSelectionDataAux(nestedMsg, maxNesting-1) {
						data = append(data, fieldName+"."+v)
					}
				}
			}
		}

		data = append(data, fieldName)
	}

	return data
}

func (p *QueryValidatePlugin) getDenyRules(fieldName string, opts *options.QueryValidate, filterType options.QueryValidate_ValueType) []options.QueryValidate_FilterOperator {
	opsAllowed := opts.GetFiltering().GetAllow()
	opsDenied := opts.GetFiltering().GetDeny()

	if len(opsAllowed) > 0 && len(opsDenied) > 0 {
		p.Fail(fieldName, ": both allow and deny options are not allowed")
	}

	if len(opsAllowed) == 0 && len(opsDenied) == 0 {
		return nil
	}

	var supportedOps []options.QueryValidate_FilterOperator
	if filterType == options.QueryValidate_NUMBER {
		supportedOps = []options.QueryValidate_FilterOperator{
			options.QueryValidate_EQ,
			options.QueryValidate_GT,
			options.QueryValidate_GE,
			options.QueryValidate_LT,
			options.QueryValidate_LE,
			options.QueryValidate_IN,
		}
	} else if filterType == options.QueryValidate_STRING {
		supportedOps = []options.QueryValidate_FilterOperator{
			options.QueryValidate_EQ,
			options.QueryValidate_MATCH,
			options.QueryValidate_GT,
			options.QueryValidate_GE,
			options.QueryValidate_LT,
			options.QueryValidate_LE,
			options.QueryValidate_IN,
			options.QueryValidate_IEQ,
		}
	} else if filterType == options.QueryValidate_BOOL {
		supportedOps = []options.QueryValidate_FilterOperator{
			options.QueryValidate_EQ,
			options.QueryValidate_IN,
		}
	}

	ops := opsAllowed
	if len(opsDenied) > 0 {
		ops = opsDenied
	}

	for _, item := range ops {
		var found bool
		for _, i := range supportedOps {
			if item == i {
				found = true
				break
			}
		}
		if !found && item != options.QueryValidate_ALL {
			p.Fail(fmt.Sprintf("'%s'filtering operator is not supported for fieldValidate '%s'", item, fieldName))
		}
	}

	var res []options.QueryValidate_FilterOperator
	if len(opsAllowed) > 0 {
	OUTER:
		for _, op := range supportedOps {
			found := false
			for _, allowedOp := range ops {
				if allowedOp == options.QueryValidate_ALL {
					res = nil
					break OUTER
				}
				if op == allowedOp {
					found = true
					break
				}
			}
			if !found {
				res = append(res, op)
			}
		}
	} else {
		res = ops
		for _, op := range ops {
			if op == options.QueryValidate_ALL {
				res = []options.QueryValidate_FilterOperator{options.QueryValidate_ALL}
				break
			}
		}
	}
	return res
}

func (p *QueryValidatePlugin) genValidateFiltering() {
	p.P(`func `, p.validateFilteringMethodName, `(methodName string, f *query.Filtering) error {`)
	p.P(`info, ok := `, p.requiredFilteringValidationVarName, `[methodName]`)
	p.P(`if !ok {`)
	p.P(`return nil`)
	p.P(`}`)
	p.P(`return options.ValidateFiltering(f, info)`)
	p.P(`}`)
}

func (p *QueryValidatePlugin) genValidateSorting() {
	p.P(`func `, p.validateSortingMethodName, `(methodName string, s *query.Sorting) error {`)
	p.P(`info, ok := `, p.requiredSortingValidationVarName, `[methodName]`)
	p.P(`if !ok {`)
	p.P(`return nil`)
	p.P(`}`)
	p.P(`return options.ValidateSorting(s, info)`)
	p.P(`}`)
}

func (p *QueryValidatePlugin) genValidateFieldSelection() {
	p.P(`func `, p.validateFieldSelectionMethodName, `(methodName string, s *query.FieldSelection) error {`)
	p.P(`info, ok := `, p.requiredFieldSelectionValidationVarName, `[methodName]`)
	p.P(`if !ok {`)
	p.P(`return nil`)
	p.P(`}`)
	p.P(`return options.ValidateFieldSelection(s, info)`)
	p.P(`}`)
}

func getQueryValidationOptions(field *descriptor.FieldDescriptorProto) *options.QueryValidate {
	if field.Options == nil {
		return nil
	}
	v, err := proto.GetExtension(field.Options, options.E_Validate)
	if err != nil {
		return nil
	}
	opts, ok := v.(*options.QueryValidate)
	if !ok {
		return nil
	}

	if opts.GetValueTypeUrl() != "" {

		if opts.Sorting == nil {
			opts.Sorting = &options.QueryValidate_Sorting{Disable: true}
		}

		if opts.FieldSelection == nil {
			opts.FieldSelection = &options.QueryValidate_FieldSelection{Disable: true}
		}
	}
	return opts
}

func (p *QueryValidatePlugin) getNestDepth(msg *generator.Descriptor) int {
	nestDepth := p.maxNesting
	if opts := p.getMessageOptions(msg.DescriptorProto); opts != nil {
		if opts.NestedFieldDepthLimit != 0 {
			nestDepth = int(opts.NestedFieldDepthLimit)
		}
	}
	return nestDepth
}

func (p *QueryValidatePlugin) getMessageOptions(msg *descriptor.DescriptorProto) *options.MessageQueryValidate {
	if msg.Options == nil {
		return nil
	}

	v, err := proto.GetExtension(msg.Options, options.E_Message)
	if err != nil {
		return nil
	}

	opts, ok := v.(*options.MessageQueryValidate)
	if !ok {
		return nil
	}
	return opts
}

func (p *QueryValidatePlugin) getMessageQueryValidationOptions(msg *descriptor.DescriptorProto) []*options.MessageQueryValidate_QueryValidateEntry {
	opts := p.getMessageOptions(msg)
	if opts == nil {
		return nil
	}

	res := make([]*options.MessageQueryValidate_QueryValidateEntry, len(opts.GetValidate()))
	for i, opt := range opts.GetValidate() {

		if opt.GetName() == "" {
			p.Fail(`empty synthetic validate option for message `, msg.GetName())
		}

		o := opt.GetValue()
		if o == nil {
			p.Fail(`empty synthetic validate option for field `, msg.GetName(), `.`, opt.GetName())
		}

		if len(o.NestedFields) > 0 {
			o.EnableNestedFields = true
		}

		if o.Sorting == nil {
			o.Sorting = &options.QueryValidate_Sorting{Disable: true}
		}

		if o.FieldSelection == nil {
			o.FieldSelection = &options.QueryValidate_FieldSelection{Disable: true}
		}

		res[i] = &options.MessageQueryValidate_QueryValidateEntry{Value: o, Name: opt.GetName()}
	}

	return res
}

func (p *QueryValidatePlugin) CleanFiles(response *plugin.CodeGeneratorResponse) {
	for i := 0; i < len(response.File); i++ {
		file := response.File[i]
		file.Content = CleanImports(file.Content)
	}
}

func (p *QueryValidatePlugin) allowNested(msgDesc *generator.Descriptor, fieldOpts *options.QueryValidate) bool {
	msgOpts := p.getMessageOptions(msgDesc.DescriptorProto)
	return p.alwaysNest || msgOpts.GetEnableNestedFields() ||
		fieldOpts.GetEnableNestedFields() || len(fieldOpts.GetNestedFields()) > 0
}
