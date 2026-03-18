import type {
  ExtractionResult,
  Observation,
  ObservationType,
  ObservationStatus,
  LowSignalResult,
  FileRef,
  Alternative,
} from './types.js';

const VALID_TYPES: ObservationType[] = ['decision', 'constraint', 'exploration', 'discovery'];
const VALID_STATUSES: ObservationStatus[] = ['adopted', 'modified', 'abandoned'];

function extractTag(xml: string, tag: string): string | null {
  const re = new RegExp(`<${tag}[^>]*>([\\s\\S]*?)</${tag}>`, 'i');
  const match = re.exec(xml);
  return match ? match[1].trim() : null;
}

function extractAllTags(xml: string, tag: string): string[] {
  const re = new RegExp(`<${tag}[^>]*>([\\s\\S]*?)</${tag}>`, 'gi');
  const results: string[] = [];
  let match;
  while ((match = re.exec(xml)) !== null) {
    const text = match[1].trim();
    if (text) results.push(text);
  }
  return results;
}

function extractFiles(xml: string): FileRef[] {
  const re = /<file\s+path="([^"]+)"\s+action="([^"]+)"\s*\/?>/gi;
  const results: FileRef[] = [];
  let match;
  while ((match = re.exec(xml)) !== null) {
    results.push({ path: match[1], action: match[2] });
  }
  return results;
}

function extractAlternatives(xml: string): Alternative[] {
  const re = /<alt\s+option="([^"]+)"\s+rejected="([^"]+)"\s*\/?>/gi;
  const results: Alternative[] = [];
  let match;
  while ((match = re.exec(xml)) !== null) {
    results.push({ option: match[1], rejected: match[2] });
  }
  return results;
}

export function parseExtractionResult(xml: string): ExtractionResult | null {
  const trimmed = xml.trim();

  // Check for low_signal
  const lowSignalMatch = /<low_signal\s+reason="([^"]+)"\s*\/?>/i.exec(trimmed);
  if (lowSignalMatch) {
    return { lowSignal: true, reason: lowSignalMatch[1] } as LowSignalResult;
  }

  // Also handle <low_signal reason="...">...</low_signal>
  const lowSignalMatch2 = /<low_signal\s+reason="([^"]+)">/i.exec(trimmed);
  if (lowSignalMatch2) {
    return { lowSignal: true, reason: lowSignalMatch2[1] } as LowSignalResult;
  }

  // Extract observation block
  const obsBlock = extractTag(trimmed, 'observation');
  if (!obsBlock) return null;

  // Parse type
  const rawType = extractTag(obsBlock, 'type');
  const type: ObservationType = VALID_TYPES.includes(rawType as ObservationType)
    ? (rawType as ObservationType)
    : 'discovery';

  // Parse status
  const rawStatus = extractTag(obsBlock, 'status');
  const status: ObservationStatus = VALID_STATUSES.includes(rawStatus as ObservationStatus)
    ? (rawStatus as ObservationStatus)
    : 'adopted';

  // Core fields
  const title = extractTag(obsBlock, 'title') || '(untitled)';
  const narrative = extractTag(obsBlock, 'narrative') || '';
  const facts = extractAllTags(obsBlock, 'fact');
  const concepts = extractAllTags(obsBlock, 'concept');
  const files = extractFiles(obsBlock);

  const observation: Observation = {
    type,
    status,
    title,
    narrative,
    facts,
    concepts,
    files,
  };

  // Type-specific fields
  if (type === 'decision') {
    const rationale = extractTag(obsBlock, 'rationale');
    if (rationale) observation.rationale = rationale;
    const alts = extractAlternatives(obsBlock);
    if (alts.length > 0) observation.alternatives = alts;
  }

  if (type === 'constraint') {
    const source = extractTag(obsBlock, 'source');
    if (source) observation.source = source;
    const impact = extractTag(obsBlock, 'impact');
    if (impact) observation.impact = impact;
  }

  if (type === 'exploration') {
    const approach = extractTag(obsBlock, 'approach');
    if (approach) observation.approach = approach;
    const outcome = extractTag(obsBlock, 'outcome');
    if (outcome) observation.outcome = outcome;
    if (status === 'abandoned') {
      const reason = extractTag(obsBlock, 'abandonment_reason');
      if (reason) observation.abandonmentReason = reason;
    }
  }

  return observation;
}
