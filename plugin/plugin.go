package plugin

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/protoc-gen-gogo/descriptor"
	"github.com/gogo/protobuf/protoc-gen-gogo/generator"
	plugin "github.com/gogo/protobuf/protoc-gen-gogo/plugin"
	"github.com/infobloxopen/atlas-app-toolkit/query"
	"github.com/infobloxopen/protoc-gen-perm/options"
)

const (
	filtering             = ".infoblox.api.Filtering"
	sorting               = ".infoblox.api.Sorting"
	permissionSuffix      = "MessagesRequiredValidation"
	methodFilteringSuffix = "MethodsRequiredFilteringValidation"
	methodPagingSuffix    = "MethodsRequiredPagingValidation"
)

// PermPlugin implements the plugin interface and creates validations for collection operation parameters code from .protos
type PermPlugin struct {
	*generator.Generator
	currentFile                            *generator.FileDescriptor
	messagePermissionsData                 string
	requiredFilteringValidationMethodsData string
	requiredSortingValidationMethodsData   string
}

func (p *PermPlugin) setFile(file *generator.FileDescriptor) {
	p.currentFile = file
	// p.Generator.SetFile(file.FileDescriptorProto)

	baseFileName := filepath.Base(file.GetName())
	p.messagePermissionsData = strings.TrimSuffix(baseFileName, filepath.Ext(baseFileName)) + permissionSuffix
	p.requiredFilteringValidationMethodsData = strings.TrimSuffix(baseFileName, filepath.Ext(baseFileName)) + methodFilteringSuffix
	p.requiredSortingValidationMethodsData = strings.TrimSuffix(baseFileName, filepath.Ext(baseFileName)) + methodPagingSuffix

}

// Name identifies the plugin
func (p *PermPlugin) Name() string {
	return "perm"
}

// Init is called once after data structures are built but before
// code generation begins.
func (p *PermPlugin) Init(g *generator.Generator) {
	p.Generator = g
}

// Generate produces the code generated by the plugin for this file,
// except for the imports, by calling the generator's methods P, In, and Out.
func (p *PermPlugin) Generate(file *generator.FileDescriptor) {
	p.setFile(file)
	p.genValidationData()
	p.genValidationFunc()
}

func (p *PermPlugin) genValidationData() {
	messagesByName := map[string]*generator.Descriptor{}
	msgRequireValidations := map[string]struct{}{}
	msgWithFilteringField := map[string]struct{}{}
	msgWithSortingField := map[string]struct{}{}

	packageName := p.currentFile.Package

	// generate message validation data
	p.P(`var `, p.messagePermissionsData, ` = map[string]map[string]options.FilteringOption{`)
	for _, msg := range p.currentFile.Messages() {
		fullMsgName := fmt.Sprintf(".%s.%s", *packageName, generator.CamelCaseSlice(msg.TypeName()))
		messagesByName[fullMsgName] = msg

		hasFiltering, hasSorting, isGenerated := p.generateMessagePermissions(msg)

		if isGenerated {
			msgRequireValidations[fullMsgName] = struct{}{}
		}
		if hasFiltering {
			msgWithFilteringField[fullMsgName] = struct{}{}
		}
		if hasSorting {
			msgWithSortingField[fullMsgName] = struct{}{}
		}
	}
	p.P(`}`)

	data := map[string]map[string]struct{}{
		p.requiredFilteringValidationMethodsData: msgWithFilteringField,
		p.requiredSortingValidationMethodsData:   msgWithSortingField,
	}

	for prefix, reqValidation := range data {
		// generate methods required validation data
		p.P(`var `, prefix, ` = map[string]string{`)
		for _, srv := range p.currentFile.GetService() {
			for _, method := range srv.GetMethod() {

				_, hasFilteringSorting := reqValidation[method.GetInputType()]
				msg, ok := messagesByName[method.GetOutputType()]
				if !ok {
					continue
				}
				msgFieldType := p.hasRequiredValidationField(msg, msgRequireValidations)
				if hasFilteringSorting && len(msgFieldType) > 0 {
					p.P(`"`, fmt.Sprintf("/%s.%s/%s", *packageName, srv.GetName(), method.GetName()), `": "`, strings.TrimLeft(msgFieldType, "."+*packageName), `",`)
				}
			}
		}
		p.P(`}`)
	}

}

func (p *PermPlugin) hasRequiredValidationField(msg *generator.Descriptor, requireValidations map[string]struct{}) string {
	for _, msgField := range msg.GetField() {
		if _, ok := requireValidations[msgField.GetTypeName()]; ok {
			return msgField.GetTypeName()
		}
	}
	return ""
}

func (p *PermPlugin) generateMessagePermissions(msg *generator.Descriptor) (bool, bool, bool) {
	msgTypeName := generator.CamelCaseSlice(msg.TypeName())
	hasFilteringField := false
	hasSortingField := false
	msgHasFieldWithPermissions := false

	for _, msgField := range msg.GetField() {

		if msgField.GetTypeName() == filtering {
			hasFilteringField = true
		}

		if msgField.GetTypeName() == sorting {
			hasSortingField = true
		}

		permissionOpts := getFieldPermissionsOption(msgField)
		if permissionOpts == nil {
			continue
		}

		msgFieldName := msgField.GetName()
		denyOps, err := getDenyOperations(msgFieldName, msgField.GetType().String(), permissionOpts)
		if err != nil {
			p.Fail(fmt.Sprintf(`Error for message '%s': %s`, msgTypeName, err.Error()))
		}

		if permissionOpts.GetDisableSorting() || len(denyOps) > 0 {
			if !msgHasFieldWithPermissions {
				msgHasFieldWithPermissions = true
				p.P(`"`, msgTypeName, `": {`)
			}
			p.P(`"`, msgFieldName, `": options.FilteringOption{DisableSorting: `, permissionOpts.GetDisableSorting(), `, Deny: []string{"`, denyOps, `"}},`)
		}
	}
	if msgHasFieldWithPermissions {
		p.P(`},`)
	}
	return hasFilteringField, hasSortingField, msgHasFieldWithPermissions
}

func getFieldPermissionsOption(field *descriptor.FieldDescriptorProto) *options.CollectionPermissions {
	if field.Options == nil {
		return nil
	}
	v, err := proto.GetExtension(field.Options, options.E_Permissions)
	if err != nil {
		return nil
	}
	opts, ok := v.(*options.CollectionPermissions)
	if !ok {
		return nil
	}
	return opts
}

// getDenyOperations - returns list of denied operations if possible or error
func getDenyOperations(fieldName string, fieldType string, permissionOpts *options.CollectionPermissions) (string, error) {
	res := []string{}

	f := permissionOpts.GetFilters()
	opsAllowed := f.GetAllow()
	opsDenied := f.GetDeny()

	if len(opsAllowed) == 0 && len(opsDenied) == 0 {
		return "", nil
	}
	if len(opsAllowed) > 0 && len(opsDenied) > 0 {
		return "", fmt.Errorf("Field '%s' contains both permission options (deny and allow), but only one is allowed", fieldName)
	}

	var supportedOps map[string]int32
	switch fieldType {
	case "TYPE_STRING":
		supportedOps = query.StringCondition_Type_value
	case "TYPE_DOUBLE", "TYPE_FLOAT",
		"TYPE_INT64", "TYPE_UINT64",
		"TYPE_INT32", "TYPE_FIXED64",
		"TYPE_FIXED32", "TYPE_UINT32",
		"TYPE_SFIXED32", "TYPE_SFIXED64",
		"TYPE_SINT32", "TYPE_SINT64":
		supportedOps = query.NumberCondition_Type_value
	default:
		return "", fmt.Errorf("Field '%s' does not support permission operations, supported only by string and numeric types", fieldName)
	}

	ops := opsAllowed
	if len(opsDenied) > 0 {
		ops = opsDenied
	}

	vals := strings.Split(ops, ",")
	for _, item := range vals {
		item := strings.TrimSpace(item)
		_, ok := supportedOps[item]
		if !ok {
			return "", fmt.Errorf("'%s' is unknown permission operation for field '%s'", item, fieldName)
		}
	}

	if ops == opsAllowed {
		for op, _ := range supportedOps {
			found := false
			for _, allowedOp := range vals {
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
		res = vals
	}
	return strings.Join(res, "\", \""), nil
}

func (p *PermPlugin) genValidationFunc() {
	p.P(`func Validate(f *query.Filtering, p *query.Sorting, methodName string) error {`)
	p.P(`perm, ok := `, p.requiredFilteringValidationMethodsData, `[methodName]`)
	p.P(`if !ok {`)
	p.P(`return nil`)
	p.P(`}`)
	p.P(`res := options.ValidateFilteringPermissions(f, perm, `, p.messagePermissionsData, `)`)
	p.P(`if res != nil { return res}`)
	p.P(`perm, ok = `, p.requiredSortingValidationMethodsData, `[methodName]`)
	p.P(`if !ok {`)
	p.P(`return nil`)
	p.P(`}`)
	p.P(`res = options.ValidateSortingPermissions(p, perm, `, p.messagePermissionsData, `)`)
	p.P(`if res != nil { return res}`)
	p.P(`return nil`)
	p.P(`}`)
}

func (p *PermPlugin) CleanFiles(response *plugin.CodeGeneratorResponse) {
	for i := 0; i < len(response.File); i++ {
		file := response.File[i]
		file.Content = CleanImports(file.Content)
	}
}
