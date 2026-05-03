import { Routes, Route } from 'react-router-dom'
import MainLayout from './components/layout/MainLayout'
import ErrorBoundary from './components/ErrorBoundary'
import { ToastViewport } from './components/Toast'
import { ThemeProvider } from './theme/ThemeProvider'
import AppsPage from './pages/AppsPage'
import AppDetailPage from './pages/AppDetailPage'
import NewAppWizard from './pages/NewAppWizard'
import PipelinesPage from './pages/PipelinesPage'
import PipelineEditorPage from './pages/PipelineEditorPage'
import RunPage from './pages/RunPage'
import DockerPage from './pages/DockerPage'
import ComposePage from './pages/ComposePage'
import KubernetesPage from './pages/KubernetesPage'
import EnvironmentsPage from './pages/EnvironmentsPage'
import HostsPage from './pages/HostsPage'
import SettingsPage from './pages/SettingsPage'
import RegistryPage from './pages/RegistryPage'
import SignInPage from './pages/SignInPage'
import SignUpPage from './pages/SignUpPage'
import Callback from './auth/Callback'
import ProtectedRoute from './auth/ProtectedRoute'

export default function App() {
  return (
    <ThemeProvider>
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
                <Routes>
                  <Route path="/pipelines/:id/edit" element={<MainLayout fullBleed><PipelineEditorPage /></MainLayout>} />
                  <Route path="/pipelines/:id/runs/:runId" element={<MainLayout fullBleed><RunPage /></MainLayout>} />
                  <Route path="/apps/new" element={<MainLayout><NewAppWizard /></MainLayout>} />
                  <Route path="/" element={<MainLayout><AppsPage /></MainLayout>} />
                  <Route path="/apps" element={<MainLayout><AppsPage /></MainLayout>} />
                  <Route path="/apps/:id" element={<MainLayout><AppDetailPage /></MainLayout>} />
                  <Route path="/pipelines" element={<MainLayout><PipelinesPage /></MainLayout>} />
                  <Route path="/docker" element={<MainLayout><DockerPage /></MainLayout>} />
                  <Route path="/docker/compose" element={<MainLayout fullBleed><ComposePage /></MainLayout>} />
                  <Route path="/kubernetes" element={<MainLayout><KubernetesPage /></MainLayout>} />
                  <Route path="/environments" element={<MainLayout><EnvironmentsPage /></MainLayout>} />
                  <Route path="/hosts" element={<MainLayout><HostsPage /></MainLayout>} />
                  <Route path="/settings" element={<MainLayout><SettingsPage /></MainLayout>} />
                  <Route path="/registry" element={<MainLayout><RegistryPage /></MainLayout>} />
                </Routes>
              </ProtectedRoute>
            }
          />
        </Routes>
      </ErrorBoundary>
    </ThemeProvider>
  )
}
