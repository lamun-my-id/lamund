<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import logo from '../assets/lamun-logo.png'

const props = defineProps<{ kind: 'privacy' | 'terms' }>()
const isPrivacy = computed(() => props.kind === 'privacy')

// Draf awal — dibuat sederhana & jujur. Owner sebaiknya review/sesuaikan sebelum
// dijadikan dokumen final. Terakhir diperbarui: 27 Agustus 2026.
const updated = '27 August 2026'
</script>

<template>
  <div class="legal-wrap">
    <header class="legal-head">
      <RouterLink to="/login" class="legal-brand">
        <img :src="logo" alt="Lamund" />
        <b>Lamund</b>
      </RouterLink>
      <nav>
        <RouterLink to="/terms" :class="{ on: !isPrivacy }">Terms</RouterLink>
        <RouterLink to="/privacy" :class="{ on: isPrivacy }">Privacy</RouterLink>
      </nav>
    </header>

    <main class="legal">
      <!-- ── Privacy Policy ── -->
      <template v-if="isPrivacy">
        <h1>Privacy Policy</h1>
        <p class="upd">Last updated: {{ updated }}</p>

        <p>This Privacy Policy explains what information Lamund ("we", "the service") collects when you use this hosting panel, and how we use it.</p>

        <h2>Information we collect</h2>
        <ul>
          <li><b>Account information</b> — the username, display name, and password (stored only as a secure hash) you provide when creating an account.</li>
          <li><b>Identity from sign-in providers</b> — when you sign in or connect with GitHub or Google, we receive and store your stable provider ID, email address, and public display name to identify your account. We never receive your password for those services.</li>
          <li><b>Access tokens</b> — if you connect GitHub or Google, we store an access token so the service can perform actions you request (e.g. deploy from a repository) on your behalf. Tokens are stored on the server and never exposed to other users.</li>
          <li><b>Usage &amp; technical data</b> — logs such as request metadata and deployment activity, used to operate and secure the service.</li>
        </ul>

        <h2>How we use it</h2>
        <ul>
          <li>To create and manage your account and authenticate you.</li>
          <li>To provide the features you use (deployments, teams, connected repositories).</li>
          <li>To keep the service secure and diagnose problems.</li>
        </ul>

        <h2>Sharing</h2>
        <p>We do not sell your personal information. Data is shared with third parties only as needed to provide the service you request — for example, calls to GitHub or Google using the access tokens you explicitly connected.</p>

        <h2>Retention &amp; deletion</h2>
        <p>We keep your data while your account is active. You may request deletion of your account and associated data by contacting us. Connected provider tokens are removed when you disconnect a provider.</p>

        <h2>Contact</h2>
        <p>For any privacy questions or deletion requests, contact <a href="mailto:wahyuwahyudin443@gmail.com">wahyuwahyudin443@gmail.com</a>.</p>
      </template>

      <!-- ── Terms of Service ── -->
      <template v-else>
        <h1>Terms of Service</h1>
        <p class="upd">Last updated: {{ updated }}</p>

        <p>By creating an account or using Lamund (the "service"), you agree to these Terms. If you do not agree, do not use the service.</p>

        <h2>Your account</h2>
        <ul>
          <li>You are responsible for activity under your account and for keeping your credentials secure.</li>
          <li>New accounts may require administrator approval before they can sign in.</li>
          <li>You must provide accurate information and not impersonate others.</li>
        </ul>

        <h2>Acceptable use</h2>
        <ul>
          <li>Do not use the service for unlawful, abusive, or malicious purposes.</li>
          <li>Do not attempt to disrupt, overload, or gain unauthorized access to the service or other users' data.</li>
          <li>You are responsible for the content you deploy and must have the rights to it.</li>
        </ul>

        <h2>Connected services</h2>
        <p>When you connect GitHub or Google, you authorize the service to act on your behalf within the scope you grant. You can disconnect a provider at any time.</p>

        <h2>Availability &amp; warranty</h2>
        <p>The service is provided "as is", without warranties of any kind. We may modify, suspend, or discontinue features at any time. To the extent permitted by law, we are not liable for any indirect or consequential damages arising from use of the service.</p>

        <h2>Changes</h2>
        <p>We may update these Terms. Continued use after changes take effect constitutes acceptance.</p>

        <h2>Contact</h2>
        <p>Questions about these Terms: <a href="mailto:wahyuwahyudin443@gmail.com">wahyuwahyudin443@gmail.com</a>.</p>
      </template>

      <footer class="legal-foot">
        <RouterLink to="/login">← Back to Lamund</RouterLink>
      </footer>
    </main>
  </div>
</template>

<style scoped>
.legal-wrap { min-height: 100vh; background: var(--bg); }
.legal-head { display: flex; align-items: center; justify-content: space-between; max-width: 760px; margin: 0 auto; padding: 20px 24px; }
.legal-brand { display: flex; align-items: center; gap: 8px; text-decoration: none; color: var(--ink); }
.legal-brand img { width: 26px; height: 26px; object-fit: contain; }
.legal-head nav { display: flex; gap: 16px; }
.legal-head nav a { color: var(--muted); text-decoration: none; font-size: 14px; font-weight: 600; }
.legal-head nav a.on { color: var(--brand); }
.legal { max-width: 760px; margin: 0 auto; padding: 12px 24px 64px; color: var(--slate); line-height: 1.65; font-size: 15px; }
.legal h1 { font-family: var(--font-display, 'Bricolage Grotesque', system-ui, sans-serif); font-size: 30px; color: var(--ink); margin: 12px 0 4px; }
.legal h2 { font-size: 18px; color: var(--ink); margin: 28px 0 8px; }
.legal .upd { color: var(--muted); font-size: 13px; margin: 0 0 20px; }
.legal ul { padding-left: 20px; margin: 8px 0; }
.legal li { margin: 6px 0; }
.legal a { color: var(--brand); }
.legal-foot { margin-top: 40px; padding-top: 20px; border-top: 1px solid var(--line); }
.legal-foot a { color: var(--muted); text-decoration: none; font-size: 14px; }
</style>
