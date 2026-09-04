import { useEffect, useState } from 'react';
import { templatesApi, type Template } from '../api/admin';
import StarChart, { type ChartRow } from '../components/list/StarChart';
import { timeAgo } from '../utils/time';

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

export default function TemplatesGalleryPage() {
  const [templates, setTemplates] = useState<Template[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    templatesApi
      .list()
      .then((list) => {
        if (cancelled) return;
        setTemplates(list ?? []);
        setLoading(false);
      })
      .catch((e: unknown) => {
        if (!cancelled) {
          setError(message(e));
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const rows: ChartRow[] = templates.map((t) => ({
    id: t.id,
    name: t.name,
    sub: t.description,
    status: t.enabled ? 'ok' : 'idle',
    meta: [t.category ?? 'uncategorised', t.enabled ? 'enabled' : 'disabled', `updated ${timeAgo(t.updatedAt)}`],
  }));

  return (
    <StarChart
      title="Templates"
      count={templates.length}
      rows={rows}
      hasThumbs={false}
      loading={loading}
      error={error}
      empty={{ text: 'No templates in the catalog yet.' }}
    />
  );
}
