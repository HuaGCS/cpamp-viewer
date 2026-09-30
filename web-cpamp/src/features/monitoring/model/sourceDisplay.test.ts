import { describe, expect, it } from 'vitest';
import { buildSourceInfoMap } from '@/utils/sourceResolver';
import {
  buildMonitoringSourceDisplay,
  isGenericMonitoringProviderLabel,
  isKeyDisambiguatedLabel,
  isProviderLikeMonitoringLabel,
  isRedundantMonitoringLabel,
} from './sourceDisplay';
import { isOpaqueMonitoringSourceId } from './opaqueSourceDisplay';
import type { MonitoringAuthMeta, MonitoringChannelMeta } from './types';

const emptyContext = {
  authMetaMap: new Map<string, MonitoringAuthMeta>(),
  channelByAuthIndex: new Map(),
};

describe('isGenericMonitoringProviderLabel', () => {
  it('treats codex and xAI aliases as generic provider labels', () => {
    expect(isGenericMonitoringProviderLabel('codex')).toBe(true);
    expect(isGenericMonitoringProviderLabel('meta')).toBe(true);
    expect(isGenericMonitoringProviderLabel('xai')).toBe(true);
    expect(isGenericMonitoringProviderLabel('XAI')).toBe(true);
    expect(isGenericMonitoringProviderLabel('x-ai')).toBe(true);
    expect(isGenericMonitoringProviderLabel('grok')).toBe(true);
    expect(isGenericMonitoringProviderLabel('antigravity')).toBe(true);
    expect(isGenericMonitoringProviderLabel('devin')).toBe(true);
    expect(isGenericMonitoringProviderLabel(' Devin ')).toBe(true);
    expect(isGenericMonitoringProviderLabel('anyrouter.top #1')).toBe(false);
  });
});

describe('isProviderLikeMonitoringLabel', () => {
  it('treats provider-equivalent names as generic without hiding distinct channel names', () => {
    expect(isProviderLikeMonitoringLabel('grok', 'xai')).toBe(true);
    expect(isProviderLikeMonitoringLabel(' WorkBuddy ', 'workbuddy')).toBe(true);
    expect(isProviderLikeMonitoringLabel('Team Relay', 'workbuddy')).toBe(false);
    expect(isProviderLikeMonitoringLabel('workbuddy #1', 'workbuddy')).toBe(false);
    expect(isProviderLikeMonitoringLabel('', 'workbuddy')).toBe(false);
    expect(isProviderLikeMonitoringLabel(null, 'workbuddy')).toBe(false);
    expect(isProviderLikeMonitoringLabel('workbuddy', null)).toBe(false);
  });
});

const opaqueSources = [
  `h:${'0123456789abcdef'.repeat(4)}`,
  'k:0123456789abcdef',
  'm:sk-1...cdef',
];

describe('isOpaqueMonitoringSourceId', () => {
  it.each(opaqueSources)('recognizes an opaque display identifier: %s', (source) => {
    expect(isOpaqueMonitoringSourceId(` ${source} `)).toBe(true);
  });

  it('accepts uppercase hex digits', () => {
    expect(isOpaqueMonitoringSourceId(`h:${'ABCDEF0123456789'.repeat(4)}`)).toBe(true);
    expect(isOpaqueMonitoringSourceId('k:ABCDEF0123456789')).toBe(true);
  });

  it.each([
    undefined,
    null,
    false,
    42,
    {},
    '',
    'm:',
    'h:readable-account',
    'k:readable-team',
    `h:${'a'.repeat(63)}`,
    `h:${'a'.repeat(65)}`,
    `h:${'g'.repeat(64)}`,
    `k:${'a'.repeat(15)}`,
    `k:${'a'.repeat(17)}`,
    'readable-account',
  ])('does not classify unrelated values as opaque: %j', (value) => {
    expect(isOpaqueMonitoringSourceId(value)).toBe(false);
  });
});

describe('key disambiguation helpers', () => {
  it('detects provider/key ordinal disambiguation labels', () => {
    expect(isKeyDisambiguatedLabel('kuaileshifu #1', 'kuaileshifu')).toBe(true);
    expect(isKeyDisambiguatedLabel('kuaileshifu #2', 'kuaileshifu')).toBe(true);
    expect(isKeyDisambiguatedLabel('kuaileshifu', 'kuaileshifu')).toBe(false);
    expect(isKeyDisambiguatedLabel('anyrouter.top #1', 'codex')).toBe(false);
    expect(isRedundantMonitoringLabel('kuaileshifu', 'kuaileshifu #1')).toBe(true);
  });
});

describe('buildMonitoringSourceDisplay', () => {
  it.each(['devin', 'workbuddy'])('prefers account identity over provider-equivalent %s labels', (provider) => {
    const display = buildMonitoringSourceDisplay(
      {
        authIndex: 'provider-account-1',
        account: 'user@example.com',
        authLabelSnapshot: provider,
        authProviderSnapshot: provider,
        channel: provider,
        source: provider,
      },
      emptyContext
    );

    expect(display.primary).toBe('use***@example.com');
    expect(display.meta).toBe(provider);
  });

  it.each(opaqueSources)('prefers a readable account over opaque labels: %s', (source) => {
    const display = buildMonitoringSourceDisplay(
      {
        account: 'readable-team-account',
        authLabelSnapshot: source,
        authProviderSnapshot: 'codex',
        channel: 'codex',
        source,
      },
      emptyContext
    );

    expect(display.primary).toBe('readable-team-account');
    expect(display.meta).toBe('codex');
  });

  it('keeps a distinct channel name ahead of the account for an unknown provider', () => {
    const display = buildMonitoringSourceDisplay(
      {
        account: 'readable-team-account',
        channel: 'Team WorkBuddy Relay',
        authProviderSnapshot: 'workbuddy',
      },
      emptyContext
    );

    expect(display.primary).toBe('Team WorkBuddy Relay');
    expect(display.meta).toBe('workbuddy');
  });

  it.each(opaqueSources)('falls back to the provider before an opaque source: %s', (source) => {
    const display = buildMonitoringSourceDisplay(
      {
        source,
        authProviderSnapshot: 'future-provider',
        channel: 'future-provider',
      },
      emptyContext
    );

    expect(display.primary).toBe('future-provider');
  });

  it.each(opaqueSources)('uses an opaque source only when readable metadata is absent: %s', (source) => {
    const display = buildMonitoringSourceDisplay({ source }, emptyContext);

    // The existing resolver can normalize an h: source into a k: identifier.
    expect(isOpaqueMonitoringSourceId(display.primary)).toBe(true);
  });

  it('keeps generic xAI provider labels secondary to the account identity', () => {
    const authMetaMap = new Map<string, MonitoringAuthMeta>([
      [
        'xai-1',
        {
          authIndex: 'xai-1',
          label: 'xai',
          account: 'oc0abcdef@yijihwjw.com',
          provider: 'xai',
          status: 'active',
          disabled: false,
          unavailable: false,
          runtimeOnly: false,
          planType: '-',
          updatedAt: '',
        },
      ],
    ]);

    const display = buildMonitoringSourceDisplay(
      {
        authIndex: 'xai-1',
        accountSnapshot: 'oc0abcdef@yijihwjw.com',
        authLabelSnapshot: 'xai',
        authProviderSnapshot: 'xai',
        channel: 'xai',
      },
      {
        authMetaMap,
        channelByAuthIndex: new Map(),
      }
    );

    expect(display.primary).toBe('oc0***@yijihwjw.com');
    expect(display.meta).toBe('xai');
    expect(display.accountMasked).toBe('oc0***@yijihwjw.com');
    expect(display.provider).toBe('xai');
  });

  it('keeps generic codex provider labels secondary to the account identity', () => {
    const authMetaMap = new Map<string, MonitoringAuthMeta>([
      [
        'codex-1',
        {
          authIndex: 'codex-1',
          label: 'codex',
          account: 'fbcabcdef@vip.qq.com',
          provider: 'codex',
          status: 'active',
          disabled: false,
          unavailable: false,
          runtimeOnly: false,
          planType: '-',
          updatedAt: '',
        },
      ],
    ]);

    const display = buildMonitoringSourceDisplay(
      {
        authIndex: 'codex-1',
        accountSnapshot: 'fbcabcdef@vip.qq.com',
        authLabelSnapshot: 'codex',
        authProviderSnapshot: 'codex',
        channel: 'codex',
      },
      {
        authMetaMap,
        channelByAuthIndex: new Map(),
      }
    );

    expect(display.primary).toBe('fbc***@vip.qq.com');
    expect(display.meta).toBe('codex');
  });

  it('still prefers non-generic channel names over the account identity', () => {
    const display = buildMonitoringSourceDisplay(
      {
        authIndex: 'relay-1',
        account: 'user@example.com',
        channel: 'anyrouter.top #1',
        authProviderSnapshot: 'codex',
      },
      emptyContext
    );

    expect(display.primary).toBe('anyrouter.top #1');
    expect(display.meta).toBe('codex');
  });

  it('prefers OpenAI-compatible multi-key disambiguation over the bare provider name', () => {
    const sourceInfoMap = buildSourceInfoMap({
      openaiCompatibility: [
        {
          name: 'kuaileshifu',
          baseUrl: 'https://api.kuaileshifu.example/v1',
          apiKeyEntries: [
            { apiKey: 'sk-openai111111aaaa', authIndex: 'kuai-auth-1' },
            { apiKey: 'sk-openai222222bbbb', authIndex: 'kuai-auth-2' },
          ],
        },
      ],
    });
    const channelByAuthIndex = new Map<string, MonitoringChannelMeta>([
      [
        'kuai-auth-1',
        {
          key: 'openai:0',
          name: 'kuaileshifu',
          baseUrl: 'https://api.kuaileshifu.example/v1',
          host: 'api.kuaileshifu.example',
          disabled: false,
          authIndices: ['kuai-auth-1', 'kuai-auth-2'],
          modelNames: [],
        },
      ],
    ]);

    const display = buildMonitoringSourceDisplay(
      {
        authIndex: 'kuai-auth-1',
        source: 'm:sk-o...aaaa',
        accountSnapshot: 'kuaileshifu',
        authLabelSnapshot: 'kuaileshifu',
        authProviderSnapshot: 'openai',
        channel: 'kuaileshifu',
      },
      {
        authMetaMap: new Map(),
        channelByAuthIndex,
        sourceInfoMap,
      }
    );

    expect(display.primary).toBe('kuaileshifu #1');
    expect(display.meta).toBe('openai');
    expect(display.channel).toBe('kuaileshifu');
    expect(display.channelHost).toBe('api.kuaileshifu.example');
  });

  it('keeps a single-key OpenAI-compatible provider name as primary', () => {
    const sourceInfoMap = buildSourceInfoMap({
      openaiCompatibility: [
        {
          name: 'kuaileshifu',
          baseUrl: 'https://api.kuaileshifu.example/v1',
          apiKeyEntries: [{ apiKey: 'sk-openai111111aaaa', authIndex: 'kuai-auth-1' }],
        },
      ],
    });
    const channelByAuthIndex = new Map<string, MonitoringChannelMeta>([
      [
        'kuai-auth-1',
        {
          key: 'openai:0',
          name: 'kuaileshifu',
          baseUrl: 'https://api.kuaileshifu.example/v1',
          host: 'api.kuaileshifu.example',
          disabled: false,
          authIndices: ['kuai-auth-1'],
          modelNames: [],
        },
      ],
    ]);

    const display = buildMonitoringSourceDisplay(
      {
        authIndex: 'kuai-auth-1',
        source: 'm:sk-o...aaaa',
        accountSnapshot: 'kuaileshifu',
        authProviderSnapshot: 'openai',
        channel: 'kuaileshifu',
      },
      {
        authMetaMap: new Map(),
        channelByAuthIndex,
        sourceInfoMap,
      }
    );

    expect(display.primary).toBe('kuaileshifu');
    expect(display.meta).toBe('openai');
  });
});
