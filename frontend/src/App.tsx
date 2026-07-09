import { lazy, Suspense } from 'react'
import { Routes, Route } from 'react-router-dom'
import ErrorBoundary from './components/ErrorBoundary'
import { ToastViewport } from './components/Toast'

// Landing-page routes stay eager so first paint is fast.
import AppsPage from './pages/AppsPage'
import SignInPage from './pages/SignInPage'
import SignUpPage from './pages/SignUpPage'
import Callback from './auth/Callback'
import ProtectedRoute from './auth/ProtectedRoute'

// All other routes are lazy-loaded so their JS is only downloaded when
// the user actually navigates to them.
const AppDetailPage = lazy(() => import('./pages/AppDetailPage'))
const NewAppWizard = lazy(() => import('./pages/NewAppWizard'))
const PipelinesPage = lazy(() => import('./pages/PipelinesPage'))
const PipelineEditorPage = lazy(() => import('./pages/PipelineEditorPage'))
const RunPage = lazy(() => import('./pages/RunPage'))
const DeploymentPage = lazy(() => import('./pages/DeploymentPage'))
const DockerPage = lazy(() => import('./pages/DockerPage'))
const ComposePage = lazy(() => import('./pages/ComposePage'))
const KubernetesPage = lazy(() => import('./pages/KubernetesPage'))
const CloudPage = lazy(() => import('./pages/CloudPage'))
const EnvironmentsPage = lazy(() => import('./pages/EnvironmentsPage'))
const HostsPage = lazy(() => import('./pages/HostsPage'))
const SettingsPage = lazy(() => import('./pages/SettingsPage'))
const RegistryPage = lazy(() => import('./pages/RegistryPage'))
const SchedulesPage = lazy(() => import('./pages/SchedulesPage'))
const NotificationTargetsPage = lazy(() => import('./pages/NotificationTargetsPage'))
const TemplatesGalleryPage = lazy(() => import('./pages/TemplatesGalleryPage'))
const AnalyticsPage = lazy(() => import('./pages/AnalyticsPage'))
const AuditLogPage = lazy(() => import('./pages/AuditLogPage'))

// Design reset (Phase 2): the cosmic layout/theme were removed for a full
// redesign. The route table and auth gates stay wired so the redesign has
// a working foundation; pages are placeholder stubs. Re-introduce a shared
// layout wrapper here when the new design lands.
function RouteLoadingFallback() {
  return <div style={{ padding: '2rem' }}>Loading…</div>
}

export default function App() {
  return (
    <ErrorBoundary>
      <ToastViewport />
      <Routes>
        <Route path="/signin" element={<SignInPage />} />
        <Route path="/signup" element={<SignUpPage />} />
        <Route path="/callback" element={<Callback />} />
        <Route
          path="/*"
          element={
            <ProtectedRoute>
              <Suspense fallback={<RouteLoadingFallback />}>
                <Routes>
                  <Route path="/pipelines/:id/edit" element={<PipelineEditorPage />} />
                  <Route path="/pipelines/:id/runs/:runId" element={<RunPage />} />
                  <Route path="/apps/:appId/deployments/:pipelineId/:runId" element={<DeploymentPage />} />
                  <Route path="/apps/new" element={<NewAppWizard />} />
                  <Route path="/" element={<AppsPage />} />
                  <Route path="/apps" element={<AppsPage />} />
                  <Route path="/apps/:id" element={<AppDetailPage />} />
                  <Route path="/pipelines" element={<PipelinesPage />} />
                  <Route path="/docker" element={<DockerPage />} />
                  <Route path="/docker/compose" element={<ComposePage />} />
                  <Route path="/kubernetes" element={<KubernetesPage />} />
                  <Route path="/cloud" element={<CloudPage />} />
                  <Route path="/environments" element={<EnvironmentsPage />} />
                  <Route path="/hosts" element={<HostsPage />} />
                  <Route path="/settings" element={<SettingsPage />} />
                  <Route path="/registry" element={<RegistryPage />} />
                  <Route path="/admin/schedules" element={<SchedulesPage />} />
                  <Route path="/admin/notifications" element={<NotificationTargetsPage />} />
                  <Route path="/admin/templates" element={<TemplatesGalleryPage />} />
                  <Route path="/analytics" element={<AnalyticsPage />} />
                  <Route path="/admin/audit" element={<AuditLogPage />} />
                </Routes>
              </Suspense>
            </ProtectedRoute>
          }
        />
      </Routes>
    </ErrorBoundary>
  )
}
