import { useDialog } from "naive-ui";

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
