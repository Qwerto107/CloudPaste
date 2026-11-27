import { h } from "vue";
import { useNotification } from "naive-ui";

export function useNotify() {
  const notification = useNotification();
  const active = new Map();

  return function notify(type, content) {
    if (!content) return;
    const key = `${type}:${content}`;
    if (active.has(key)) return;
    if (active.size >= 3) {
      const [oldKey, oldest] = active.entries().next().value;
      active.delete(oldKey);
      oldest.destroy();
    }
    const notice = notification[type]({
      content: () =>
        h("span", { role: type === "error" ? "alert" : "status" }, content),
      duration: type === "error" ? 6000 : 3000,
      closable: true,
      onAfterLeave: () => {
        if (active.get(key) === notice) active.delete(key);
      },
    });
    active.set(key, notice);
  };
}
