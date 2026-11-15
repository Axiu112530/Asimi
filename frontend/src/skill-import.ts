import { defineComponent, h, ref } from 'vue';
import { t } from './i18n';
import { icon } from './ui';

export const SkillImport = defineComponent({
  props: { disabled: Boolean }, emits: ['import'],
  setup(props, { emit }) {
    const name = ref('');
    function choose(event: Event) {
      const input = event.target as HTMLInputElement;
      const file = input.files?.[0];
      input.value = '';
      if (props.disabled || !file) return;
      name.value = file.name;
      emit('import', file);
    }
    return () => h('div', { class: 'skill-import-control' }, [
      h('label', { class: ['button secondary skill-import-trigger', { 'is-disabled': props.disabled }] }, [
        icon('upload', 16), h('span', t('cognitionLayout.choosePackage')),
        h('input', { type: 'file', accept: '.json,application/json', disabled: props.disabled,
          'aria-label': t('cognition.import'), 'data-testid': 'skill-import-input', onChange: choose }),
      ]),
      h('span', { class: 'skill-import-filename', title: name.value || undefined, 'data-testid': 'skill-import-filename' }, name.value || t('cognitionLayout.packageHint')),
    ]);
  },
});
