import { useDialog } from "naive-ui";
import { h } from "vue";

export function useConfirmation() {
  const dialog = useDialog();
  // 用同一套主题显示确认框；关闭、取消和点击遮罩都视为不执行。
  return (content) =>
    new Promise((resolve) =>
      dialog.warning({
        title: "请确认操作",
        content,
        positiveText: "确认",
        negativeText: "取消",
        onPositiveClick: () => {
          resolve(true);
        },
        onNegativeClick: () => {
          resolve(false);
        },
        onClose: () => {
          resolve(false);
        },
        onMaskClick: () => {
          resolve(false);
        },
      }),
    );
}

export function useFileDeletionConfirmation() {
  const dialog = useDialog();
  return (name) =>
    new Promise((resolve) => {
      let mode = "both";
      dialog.warning({
        title: "删除文件分享",
        content: () =>
          h("div", [
            h("p", `确定删除“${name}”？`),
            h("label", [
              h("input", {
                type: "checkbox",
                checked: true,
                onChange: (event) => {
                  mode = event.target.checked ? "both" : "record_only";
                },
              }),
              "同时删除存储中的原文件",
            ]),
            h(
              "p",
              "勾选后将删除原文件，合集会删除全部子文件；取消勾选则仅删除分享记录。",
            ),
          ]),
        positiveText: "删除",
        negativeText: "取消",
        onPositiveClick: () => resolve(mode),
        onNegativeClick: () => resolve(null),
        onClose: () => resolve(null),
        onMaskClick: () => resolve(null),
      });
    });
}
