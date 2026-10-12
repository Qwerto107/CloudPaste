import { computed, inject, markRaw } from "vue";

export const disabledKey = Symbol("表单禁用状态");
export function useDisabled(props) {
  const parent = inject(
    disabledKey,
    computed(() => false),
  );
  return computed(() => props.disabled || parent.value);
}

// 只有组件库的样式挂载目标获得本次页面的 nonce，不修改全局 DOM 方法。
export const styleNonce =
  document.querySelector('meta[property="csp-nonce"]')?.nonce || "";
export const styleMountTarget = markRaw({
  querySelector: (selector) => document.head.querySelector(selector),
  insertBefore(node, reference) {
    if (node.nodeName === "STYLE" && styleNonce) node.nonce = styleNonce;
    return document.head.insertBefore(node, reference);
  },
  appendChild(node) {
    if (node.nodeName === "STYLE" && styleNonce) node.nonce = styleNonce;
    return document.head.appendChild(node);
  },
});

// VueUC 的底层布局不接受 Naive UI 的样式挂载目标。
// 提前挂载其固定布局规则并保留原样式 ID，让组件复用带 nonce 的样式，避免弹层错位。
const popupStyles = {
  "vueuc/binder": `.v-binder-follower-container{position:absolute;left:0;right:0;top:0;height:0;pointer-events:none;z-index:auto}
    .v-binder-follower-content{position:absolute;z-index:auto}
    .v-binder-follower-content>*{pointer-events:all}`,
  "vueuc/virtual-list": `.v-vl{max-height:inherit;height:100%;overflow:auto;min-width:1px}
    .v-vl:not(.v-vl--show-scrollbar){scrollbar-width:none}
    .v-vl:not(.v-vl--show-scrollbar)::-webkit-scrollbar,.v-vl:not(.v-vl--show-scrollbar)::-webkit-scrollbar-track-piece,.v-vl:not(.v-vl--show-scrollbar)::-webkit-scrollbar-thumb{width:0;height:0;display:none}`,
  "vueuc/x-scroll": `.v-x-scroll{overflow:auto;scrollbar-width:none}
    .v-x-scroll::-webkit-scrollbar{width:0;height:0}`,
  "vueuc/overflow": "[v-hidden]{display:none!important}",
};
for (const [id, css] of Object.entries(popupStyles)) {
  if (document.head.querySelector(`style[cssr-id="${id}"]`)) continue;
  const style = document.createElement("style");
  style.setAttribute("cssr-id", id);
  style.textContent = css;
  styleMountTarget.appendChild(style);
}
