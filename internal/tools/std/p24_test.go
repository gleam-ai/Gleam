package std

import (
	"context"
	"testing"

	"gleam/pkg/types"
)

type fakeMemDeleter struct {
	fakeMem
	deleted map[string]bool
}

func (f *fakeMemDeleter) SoftDelete(id string) bool {
	if !f.deleted[id] {
		f.deleted[id] = true
		return true
	}
	return false
}

func TestMemDeleteTool(t *testing.T) {
	store := &fakeMemDeleter{deleted: map[string]bool{}}
	id, _ := store.Remember("该被忘掉的条目", nil)
	tool := NewMemDelete(store)

	if tool.Permission() != types.PermissionUserApproved {
		t.Errorf("memory.delete 权限 = %v，应为审批级", tool.Permission())
	}

	out, err := tool.Execute(context.Background(), map[string]any{"id": id})
	if err != nil {
		t.Fatal(err)
	}
	if out.(map[string]any)["deleted"] != true {
		t.Errorf("out = %v", out)
	}
	if _, err := tool.Execute(context.Background(), map[string]any{"id": id}); err == nil {
		t.Error("重复删除应报错")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("缺 id 应报错")
	}
}

func TestMemDeleteTool_UnsupportedStore(t *testing.T) {
	tool := NewMemDelete(&fakeMem{})
	if _, err := tool.Execute(context.Background(), map[string]any{"id": "x"}); err == nil {
		t.Error("不支持的存储应报错")
	}
}
