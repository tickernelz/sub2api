export function isUpstreamBillingProbeCapable(platform: string, type: string): boolean {
  return type === 'apikey' && platform !== 'perch'
}
