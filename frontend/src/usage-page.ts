import { defineComponent, h, ref, watch } from 'vue';
import { metrics } from './ai-pages';
import { ai } from './ai-store';
import type { AIUsageRecord } from './ai-types';
import { call, ServiceError } from './bridge';
import { date, i18n, number, t } from './i18n';
import { state } from './store';
import { button, errorText } from './ui';

interface UsageBucket {
  providerId: string;
  providerName: string;
  modelId: string;
  currency: string;
  requests: number;
  inputKnown: number;
  outputKnown: number;
  totalKnown: number;
  costKnown: number;
  inputTokens: number;
  outputTokens: number;
  totalTokens: number;
  estimatedCost: number;
}

const periods = ['today', 'threeDays', 'thisWeek', 'thisMonth', 'lastMonth', 'allTime'] as const;
type Period = typeof periods[number];

function dateRange(period: Period, now = new Date()) {
  const year = now.getFullYear();
  const month = now.getMonth();
  const day = now.getDate();
  const until = new Date(year, month, day + 1);
  let from: Date;
  switch (period) {
    case 'threeDays': from = new Date(year, month, day - 2); break;
    case 'thisWeek': from = new Date(year, month, day - (now.getDay() + 6) % 7); break;
    case 'thisMonth': from = new Date(year, month, 1); break;
    case 'lastMonth': return { from: new Date(year, month - 1, 1).toISOString(), until: new Date(year, month, 1).toISOString() };
    case 'allTime': return { from: '0001-01-01T00:00:00.000Z', until: until.toISOString() };
    default: from = new Date(year, month, day);
  }
  return { from: from.toISOString(), until: until.toISOString() };
}

function usageTotals(buckets: UsageBucket[]) {
  let requests = 0, input = 0, output = 0, total = 0;
  let inputKnown = 0, outputKnown = 0, totalKnown = 0, costKnown = 0;
  const costs = new Map<string, number>();
  for (const row of buckets) {
    requests += row.requests;
    input += row.inputTokens;
    output += row.outputTokens;
    total += row.totalTokens;
    inputKnown += row.inputKnown;
    outputKnown += row.outputKnown;
    totalKnown += row.totalKnown;
    costKnown += row.costKnown;
    if (row.costKnown) costs.set(row.currency, (costs.get(row.currency) ?? 0) + row.estimatedCost);
  }
  function display(value: number, known: number) {
    if (!requests || known === requests) return number(value);
    if (!known) return t('common.unknown');
    return t('usage.partial', { value: number(value), count: number(requests - known) });
  }
  const decimal = new Intl.NumberFormat(i18n.global.locale.value, { minimumFractionDigits: 2, maximumFractionDigits: 6 });
  const amounts = [...costs].map(([currency, cost]) => cost > 0 && cost < 0.000001
    ? '<' + decimal.format(0.000001) + ' ' + currency
    : decimal.format(cost) + ' ' + currency).join(' · ');
  return {
    requests: number(requests),
    inputTokens: display(input, inputKnown),
    outputTokens: display(output, outputKnown),
    totalTokens: display(total, totalKnown),
    estimatedCost: amounts ? (costKnown === requests ? amounts : t('usage.partial', { value: amounts, count: number(requests - costKnown) }))
      : requests ? t('common.unknown') : '—',
  };
}

function summary(buckets: UsageBucket[]) {
  return h('dl', { class: 'usage-totals', 'data-testid': 'usage-totals' },
    Object.entries(usageTotals(buckets)).map(([key, value]) => h('div', { key }, [
      h('dt', t('ai.' + key)), h('dd', { 'data-testid': 'usage-' + key }, value),
    ])));
}

function breakdown(buckets: UsageBucket[]) {
  const groups = new Map<string, UsageBucket[]>();
  for (const row of buckets) {
    const key = JSON.stringify([row.providerId, row.modelId]);
    const rows = groups.get(key) ?? [];
    rows.push(row);
    groups.set(key, rows);
  }
  return h('div', { class: 'usage-table-wrap' }, h('table', { class: 'usage-table' }, [
    h('thead', h('tr', ['provider', 'model', 'requests', 'totalTokens', 'estimatedCost'].map(key =>
      h('th', { scope: 'col', key }, t('ai.' + key))))),
    h('tbody', [...groups].map(([key, rows]) => {
      const { providerName, providerId, modelId } = rows[0];
      const modelName = ai.profiles.find(item => item.id === providerId)?.models.find(item => item.id === modelId)?.displayName || modelId;
      const totals = usageTotals(rows);
      return h('tr', { key }, [
        h('th', { scope: 'row' }, providerName || t('usage.unknownProvider')),
        h('td', modelName), h('td', totals.requests), h('td', totals.totalTokens), h('td', totals.estimatedCost),
      ]);
    })),
  ]));
}

export const UsagePage = defineComponent({
  props: { developer: Boolean },
  setup(props) {
    const period = ref<Period>('thisWeek');
    const buckets = ref<UsageBucket[]>([]);
    const loading = ref(false);
    const error = ref('');
    const recent = ref<AIUsageRecord[]>([]);
    const expanded = ref(false);
    const recentLoading = ref(false);
    const recentError = ref('');
    const refresh = ref(0);

    watch(() => [period.value, ai.usageRevision, state.connected, refresh.value] as const, async ([value, , connected], _, cleanup) => {
      let current = true;
      cleanup(() => { current = false; });
      loading.value = connected;
      error.value = '';
      if (!connected) return;
      try {
        const rows = await call<UsageBucket[]>('GetUsageSummary', dateRange(value));
        if (current) buckets.value = rows;
      } catch (cause) {
        if (current) error.value = cause instanceof ServiceError ? cause.code : 'app.unexpected';
      } finally {
        if (current) loading.value = false;
      }
    }, { immediate: true });

    watch(() => [props.developer && expanded.value, state.connected, ai.usageRevision, refresh.value] as const, async ([open, connected], _, cleanup) => {
      let current = true;
      cleanup(() => { current = false; });
      recent.value = [];
      recentError.value = '';
      recentLoading.value = open && connected;
      if (!open || !connected) return;
      try {
        const rows = await call<AIUsageRecord[]>('GetRecentUsage', dateRange('today'));
        if (current) recent.value = rows;
      } catch (cause) {
        if (current) recentError.value = cause instanceof ServiceError ? cause.code : 'app.unexpected';
      } finally {
        if (current) recentLoading.value = false;
      }
    });

    return () => h('div', { class: 'page-scroll' }, h('div', { class: 'page ai-usage', 'data-testid': 'ai-usage' }, [
      h('header', { class: 'page-heading' }, [
        h('div', [h('h1', t('navigation.usage')), h('p', t('usage.subtitle'))]),
        button('common.refresh', () => { refresh.value++; }, { class: 'button secondary', disabled: !state.connected || loading.value }),
      ]),
      h('label', { class: 'ai-field usage-range' }, [
        h('span', t('usage.range')),
        h('select', {
          value: period.value, 'data-testid': 'usage-period',
          onChange: (event: Event) => { period.value = (event.target as HTMLSelectElement).value as Period; },
        }, periods.map(value => h('option', { value, key: value }, t('usage.period.' + value)))),
      ]),
      h('section', { class: 'usage-overview', 'aria-busy': loading.value }, [
        loading.value ? h('p', { role: 'status' }, t('usage.loading')) : error.value
          ? h('p', { role: 'alert', class: 'ai-error' }, errorText(error.value))
          : !state.connected ? h('p', { role: 'status' }, t('usage.unavailable'))
          : [
              summary(buckets.value),
              h('section', { class: 'usage-breakdown', 'data-testid': 'usage-breakdown' }, [
                h('h2', t('usage.breakdown')),
                buckets.value.length ? breakdown(buckets.value) : h('p', { class: 'muted' }, t('usage.emptyRange')),
              ]),
            ],
      ]),
      props.developer ? h('details', {
        class: 'usage-diagnostics', 'data-testid': 'usage-diagnostics',
        onToggle: (event: Event) => { expanded.value = (event.currentTarget as HTMLDetailsElement).open; },
      }, [
        h('summary', t('usage.diagnostics')),
        h('p', { class: 'micro muted' }, t('usage.diagnosticsHint')),
        recentLoading.value ? h('p', { role: 'status' }, t('usage.loading')) : recentError.value
          ? h('p', { role: 'alert', class: 'ai-error' }, errorText(recentError.value))
          : recent.value.length ? recent.value.map(record => {
            const name = ai.profiles.find(item => item.id === record.providerId)?.name || t('usage.unknownProvider');
            return h('details', { key: record.requestId, 'data-testid': 'usage-request' }, [
              h('summary', [h('time', { datetime: record.createdAt }, date(record.createdAt)), ' · ' + name + ' · ' + record.modelId]),
              metrics(record),
              record.errorCode ? h('p', { class: 'ai-error' }, errorText(record.errorCode)) : null,
            ]);
          }) : h('p', { class: 'muted' }, t('usage.emptyRange')),
      ]) : null,
    ]));
  },
});
