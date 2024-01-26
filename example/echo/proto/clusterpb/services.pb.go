// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.31.0
// 	protoc        v3.21.12
// source: cluster/services.proto

package clusterpb

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type Services int32

const (
	Services_UNSPECIFIED Services = 0
	Services_ECHO        Services = 1
)

// Enum value maps for Services.
var (
	Services_name = map[int32]string{
		0: "UNSPECIFIED",
		1: "ECHO",
	}
	Services_value = map[string]int32{
		"UNSPECIFIED": 0,
		"ECHO":        1,
	}
)

func (x Services) Enum() *Services {
	p := new(Services)
	*p = x
	return p
}

func (x Services) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (Services) Descriptor() protoreflect.EnumDescriptor {
	return file_cluster_services_proto_enumTypes[0].Descriptor()
}

func (Services) Type() protoreflect.EnumType {
	return &file_cluster_services_proto_enumTypes[0]
}

func (x Services) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use Services.Descriptor instead.
func (Services) EnumDescriptor() ([]byte, []int) {
	return file_cluster_services_proto_rawDescGZIP(), []int{0}
}

var File_cluster_services_proto protoreflect.FileDescriptor

var file_cluster_services_proto_rawDesc = []byte{
	0x0a, 0x16, 0x63, 0x6c, 0x75, 0x73, 0x74, 0x65, 0x72, 0x2f, 0x73, 0x65, 0x72, 0x76, 0x69, 0x63,
	0x65, 0x73, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x07, 0x63, 0x6c, 0x75, 0x73, 0x74, 0x65,
	0x72, 0x2a, 0x25, 0x0a, 0x08, 0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x73, 0x12, 0x0f, 0x0a,
	0x0b, 0x55, 0x4e, 0x53, 0x50, 0x45, 0x43, 0x49, 0x46, 0x49, 0x45, 0x44, 0x10, 0x00, 0x12, 0x08,
	0x0a, 0x04, 0x45, 0x43, 0x48, 0x4f, 0x10, 0x01, 0x42, 0x3a, 0x5a, 0x38, 0x67, 0x69, 0x74, 0x68,
	0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x6a, 0x6f, 0x79, 0x70, 0x61, 0x72, 0x74, 0x79, 0x2f,
	0x6e, 0x6f, 0x64, 0x65, 0x68, 0x75, 0x62, 0x2f, 0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x2f,
	0x65, 0x63, 0x68, 0x6f, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x63, 0x6c, 0x75, 0x73, 0x74,
	0x65, 0x72, 0x70, 0x62, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_cluster_services_proto_rawDescOnce sync.Once
	file_cluster_services_proto_rawDescData = file_cluster_services_proto_rawDesc
)

func file_cluster_services_proto_rawDescGZIP() []byte {
	file_cluster_services_proto_rawDescOnce.Do(func() {
		file_cluster_services_proto_rawDescData = protoimpl.X.CompressGZIP(file_cluster_services_proto_rawDescData)
	})
	return file_cluster_services_proto_rawDescData
}

var file_cluster_services_proto_enumTypes = make([]protoimpl.EnumInfo, 1)
var file_cluster_services_proto_goTypes = []interface{}{
	(Services)(0), // 0: cluster.Services
}
var file_cluster_services_proto_depIdxs = []int32{
	0, // [0:0] is the sub-list for method output_type
	0, // [0:0] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() { file_cluster_services_proto_init() }
func file_cluster_services_proto_init() {
	if File_cluster_services_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_cluster_services_proto_rawDesc,
			NumEnums:      1,
			NumMessages:   0,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_cluster_services_proto_goTypes,
		DependencyIndexes: file_cluster_services_proto_depIdxs,
		EnumInfos:         file_cluster_services_proto_enumTypes,
	}.Build()
	File_cluster_services_proto = out.File
	file_cluster_services_proto_rawDesc = nil
	file_cluster_services_proto_goTypes = nil
	file_cluster_services_proto_depIdxs = nil
}
