package main

import (
	"fmt"

	"github.com/joyparty/gokit"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

const (
	optionServiceCode  = "service_code"
	optionReplyCode    = "reply_code"
	optionReplyService = "reply_service"
)

var extTypes = new(protoregistry.Types)

func main() {
	protogen.Options{}.Run(func(gen *protogen.Plugin) error {
		// The type information for all extensions is in the source files,
		// so we need to extract them into a dynamically created protoregistry.Types.
		for _, file := range gen.Files {
			gokit.Must(registerAllExtensions(extTypes, file.Desc))
		}

		for _, file := range gen.Files {
			if file.Generate {
				generateFile(gen, file)
			}
		}
		return nil
	})
}

func generateFile(gen *protogen.Plugin, file *protogen.File) *protogen.GeneratedFile {
	g := gen.NewGeneratedFile(
		fmt.Sprintf("%s_nodehub.pb.go", file.GeneratedFilenamePrefix),
		file.GoImportPath,
	)

	g.P("// Code generated by protoc-gen-go-nodehub. DO NOT EDIT.")
	if file.Proto.GetOptions().GetDeprecated() {
		g.P("// ", file.Desc.Path(), " is a deprecated file.")
	} else {
		g.P("// source: ", file.Desc.Path())
	}
	g.P()
	g.P("package ", file.GoPackageName)
	g.P()

	var ok bool
	ok = genMethodReplyCodes(file, g) || ok
	ok = genPackMessages(file, g) || ok
	if !ok {
		g.Skip()
	}
	return g
}

func registerAllExtensions(extTypes *protoregistry.Types, descs interface {
	Messages() protoreflect.MessageDescriptors
	Extensions() protoreflect.ExtensionDescriptors
},
) error {
	mds := descs.Messages()
	for i := 0; i < mds.Len(); i++ {
		registerAllExtensions(extTypes, mds.Get(i))
	}
	xds := descs.Extensions()
	for i := 0; i < xds.Len(); i++ {
		if err := extTypes.RegisterExtension(dynamicpb.NewExtensionType(xds.Get(i))); err != nil {
			return err
		}
	}
	return nil
}
