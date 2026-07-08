import { PlanetOrb } from './PlanetOrb';
import { planetKindOf } from './helpers';

export function KindBadge({ kind }: { kind: string }) {
  return <PlanetOrb kind={kind} size={26} ring={planetKindOf(kind) === 'approval'} />;
}
