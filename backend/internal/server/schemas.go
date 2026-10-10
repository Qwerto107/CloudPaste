package server

import "net/http"

type formField struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Required bool           `json:"required"`
	LabelKey string         `json:"labelKey"`
	Default  any            `json:"defaultValue,omitempty"`
	UI       map[string]any `json:"ui,omitempty"`
}

func field(name, kind, label string, required bool, defaultValue any) formField {
	return formField{Name: name, Type: kind, Required: required, LabelKey: label, Default: defaultValue}
}

func storageMetadata() []map[string]any {
	// 只公布已实现的能力和字段，具体配置可单独设为只读。
	definitions := []struct {
		Type, Name string
		Fields     []formField
	}{
		{"LOCAL", "本地文件", []formField{field("root_path", "string", "admin.storage.fields.local.root_path", true, "/files")}},
		{"S3", "S3 兼容存储", []formField{
			field("endpoint_url", "string", "admin.storage.fields.endpoint_url", false, nil),
			field("bucket_name", "string", "admin.storage.fields.bucket_name", true, nil),
			field("region", "string", "admin.storage.fields.region", false, "us-east-1"),
			field("access_key_id", "secret", "admin.storage.fields.access_key_id", true, nil),
			field("secret_access_key", "secret", "admin.storage.fields.secret_access_key", true, nil),
			field("force_path_style", "boolean", "admin.storage.fields.force_path_style", false, true),
		}},
	}
	items := []map[string]any{}
	for _, d := range definitions {
		d.Fields = append(d.Fields, field("readonly", "boolean", "admin.storage.fields.local.readonly", false, false))
		names := []string{}
		for _, f := range d.Fields {
			names = append(names, f.Name)
		}
		items = append(items, map[string]any{
			"type": d.Type, "displayName": d.Name,
			"capabilities": []string{"reader", "writer", "proxy"},
			"ui":           map[string]any{"icon": "folder", "badgeClass": "bg-blue-100 text-blue-800"},
			"configSchema": map[string]any{
				"fields": d.Fields,
				"layout": map[string]any{
					"groups":        []map[string]any{{"name": "basic", "titleKey": "admin.storage.groups.basic", "fields": names}},
					"summaryFields": names,
				},
			},
		})
	}
	return items
}
func (a *App) storageTypes(w http.ResponseWriter, r *http.Request) error {
	a.write(w, 200, storageMetadata(), "获取存储类型成功")
	return nil
}

func (a *App) mountSchema(w http.ResponseWriter, r *http.Request) error {
	fields := []formField{
		field("name", "string", "admin.mount.form.name", true, nil),
		field("storage_config_id", "select", "admin.mount.form.storageConfig", true, nil),
		field("mount_path", "string", "admin.mount.form.mountPath", true, "/files"),
		field("root_path", "string", "admin.storage.fields.local.root_path", false, "/"),
		field("remark", "textarea", "admin.mount.form.remark", false, nil),
		field("sort_order", "number", "admin.mount.form.sortOrder", false, 0),
		field("is_active", "boolean", "admin.mount.form.isActive", false, true),
	}
	fields[1].UI = map[string]any{"dynamicOptions": "storageConfigs", "showTypeLabel": true}
	view := map[string]any{
		"fields": fields,
		"layout": map[string]any{
			"groups": []map[string]any{{
				"id": "basic", "titleKey": "admin.mount.groups.basic",
				"fields": []string{"name", "storage_config_id", "mount_path", "root_path", "remark", "sort_order", "is_active"},
			}},
		},
	}
	a.write(w, 200, view, "获取挂载配置成功")
	return nil
}
