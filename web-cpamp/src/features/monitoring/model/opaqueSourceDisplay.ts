/** Redaction notices describe hidden data rather than an account or source. */
export const isRedactedMonitoringLabel = (value: string): boolean =>
  value.trim() === '敏感错误详情已隐藏' || value.trim() === '内部错误详情已隐藏';

/** Recognize display-only source identifiers without hashing or normalizing keys. */
export const isOpaqueMonitoringSourceId = (value: unknown): boolean => {
  if (typeof value !== 'string') return false;
  const trimmed = value.trim();
  return (
    /^h:[0-9a-fA-F]{64}$/.test(trimmed) ||
    /^k:[0-9a-fA-F]{16}$/.test(trimmed) ||
    (trimmed.startsWith('m:') && trimmed.length > 2)
  );
};
