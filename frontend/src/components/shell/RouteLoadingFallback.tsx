import Skeleton from '../ui/Skeleton';

/** Skeleton shown while a lazy route chunk loads — matches a star-chart list's layout. */
export default function RouteLoadingFallback() {
  return (
    <div className="route-skeleton" role="status" aria-live="polite" aria-label="Loading">
      <Skeleton width={180} height={24} />
      <div className="route-skeleton-rows">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="route-skeleton-row">
            <Skeleton width={72} height={40} radius={4} />
            <Skeleton width={i % 2 ? 220 : 160} height={14} />
            <span style={{ flex: 1 }} />
            <Skeleton width={96} height={12} />
          </div>
        ))}
      </div>
    </div>
  );
}
