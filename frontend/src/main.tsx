import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import { OIDCProvider } from './auth/OIDCProvider'
import './index.css'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <OIDCProvider>
        <App />
      </OIDCProvider>
    </BrowserRouter>
  </React.StrictMode>,
)
