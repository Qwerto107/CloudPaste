<script setup>
import { ref } from "vue";

const password = defineModel("password", { type: String, required: true });
const maxViews = defineModel("maxViews", {
  type: [Number, String],
  required: true,
});
const passwordEnabled = ref(Boolean(password.value));
const limitEnabled = ref(Number(maxViews.value) > 0);

function togglePassword(event) {
  passwordEnabled.value = event.target.checked;
  if (!passwordEnabled.value) password.value = "";
}
function toggleLimit(event) {
  limitEnabled.value = event.target.checked;
  maxViews.value = limitEnabled.value ? 5 : 0;
}
</script>

<template>
  <div class="share-access-fields">
    <label class="switch-row">
      <span>访问密码</span>
      <input
        class="page-switch"
        type="checkbox"
        role="switch"
        :checked="passwordEnabled"
        @change="togglePassword"
      />
    </label>
    <div
      class="field-reveal"
      :class="{ expanded: passwordEnabled }"
      :inert="!passwordEnabled"
    >
      <div>
        <UiInput
          v-model="password"
          :disabled="!passwordEnabled"
          type="password"
          aria-label="访问密码"
          placeholder="接收者需输入此密码"
          autocomplete="new-password"
          maxlength="72"
        />
      </div>
    </div>
    <label class="switch-row">
      <span>限制查看次数</span>
      <input
        class="page-switch"
        type="checkbox"
        role="switch"
        :checked="limitEnabled"
        @change="toggleLimit"
      />
    </label>
    <div
      class="field-reveal"
      :class="{ expanded: limitEnabled }"
      :inert="!limitEnabled"
    >
      <div>
        <div class="share-name-input share-limit-input">
          <UiInput
            v-model.number="maxViews"
            :disabled="!limitEnabled"
            type="number"
            min="1"
            step="1"
            required
            aria-label="最多查看次数"
          />
          <span>次后失效</span>
        </div>
      </div>
    </div>
  </div>
</template>
