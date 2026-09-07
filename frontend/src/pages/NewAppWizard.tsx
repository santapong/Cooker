import { useSearchParams } from 'react-router-dom';
import ComposeImportWizard from './ComposeImportWizard';
import SingleAppWizard from './SingleAppWizard';

export default function NewAppWizard() {
  const [params] = useSearchParams();
  return params.get('mode') === 'dockerfile' ? <SingleAppWizard /> : <ComposeImportWizard />;
}
