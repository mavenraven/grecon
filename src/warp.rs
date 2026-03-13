use std::ffi::c_void;
use std::ptr;

use core_foundation::{
    base::{CFType, TCFType},
    array::CFArray,
    string::CFString,
};

type AXUIElementRef = *const c_void;
type AXError = i32;
const K_AX_ERROR_SUCCESS: AXError = 0;

#[link(name = "ApplicationServices", kind = "framework")]
extern "C" {
    fn AXUIElementCreateApplication(pid: i32) -> AXUIElementRef;
    fn AXUIElementCopyAttributeValue(
        element: AXUIElementRef,
        attribute: core_foundation_sys::string::CFStringRef,
        value: *mut *const c_void,
    ) -> AXError;
    fn AXUIElementPerformAction(
        element: AXUIElementRef,
        action: core_foundation_sys::string::CFStringRef,
    ) -> AXError;
    fn AXIsProcessTrusted() -> bool;
}

fn get_attr_value(element: AXUIElementRef, attr: &str) -> Option<*const c_void> {
    let cf_attr = CFString::new(attr);
    let mut value: *const c_void = ptr::null();
    let err = unsafe {
        AXUIElementCopyAttributeValue(element, cf_attr.as_concrete_TypeRef(), &mut value)
    };
    if err == K_AX_ERROR_SUCCESS && !value.is_null() {
        Some(value)
    } else {
        None
    }
}

fn get_attr_string(element: AXUIElementRef, attr: &str) -> Option<String> {
    get_attr_value(element, attr).map(|v| {
        let cf: CFString = unsafe { TCFType::wrap_under_get_rule(v as _) };
        cf.to_string()
    })
}

fn get_attr_children(element: AXUIElementRef) -> Vec<AXUIElementRef> {
    match get_attr_value(element, "AXChildren") {
        Some(v) => {
            let arr: CFArray<CFType> = unsafe { TCFType::wrap_under_get_rule(v as _) };
            (0..arr.len())
                .map(|i| {
                    let item = arr.get(i).unwrap();
                    item.as_CFTypeRef() as AXUIElementRef
                })
                .collect()
        }
        None => vec![],
    }
}

fn perform_action(element: AXUIElementRef, action: &str) -> bool {
    let cf_action = CFString::new(action);
    let err = unsafe { AXUIElementPerformAction(element, cf_action.as_concrete_TypeRef()) };
    err == K_AX_ERROR_SUCCESS
}

/// Check if this process has accessibility permissions.
pub fn is_accessibility_trusted() -> bool {
    unsafe { AXIsProcessTrusted() }
}

/// Send Cmd+N keystroke to Warp via osascript to jump to tab N (1-9).
pub fn switch_to_tab_number(n: u8) {
    if !(1..=9).contains(&n) {
        return;
    }
    let script = format!(
        r#"tell application "System Events" to keystroke "{n}" using command down"#
    );
    let _ = std::process::Command::new("osascript")
        .args(["-e", &script])
        .output();
}

/// Probe all Warp tabs by switching to each via Cmd+N and reading the window title.
/// Returns Vec of (tab_position, title) where tab_position is 1-indexed.
/// Switches back to `return_to_tab` when done.
pub fn probe_tab_titles(return_to_tab: u8) -> Vec<(u8, String)> {
    let script = r#"
set tabTitles to ""
tell application "System Events"
    set lastTitle to ""
    repeat with n from 1 to 9
        keystroke (n as string) using command down
        delay 0.05
        tell process "Warp"
            set t to name of window 1
        end tell
        if n > 1 and t = lastTitle then
            exit repeat
        end if
        set tabTitles to tabTitles & n & "\t" & t & "\n"
        set lastTitle to t
    end repeat
end tell
return tabTitles
"#;

    let output = std::process::Command::new("osascript")
        .args(["-e", script])
        .output();

    let result = match output {
        Ok(o) => String::from_utf8_lossy(&o.stdout).to_string(),
        Err(_) => return vec![],
    };

    let tabs: Vec<(u8, String)> = result
        .lines()
        .filter_map(|line| {
            let mut parts = line.splitn(2, '\t');
            let pos: u8 = parts.next()?.parse().ok()?;
            let title = parts.next()?.to_string();
            Some((pos, title))
        })
        .collect();

    // Switch back to the original tab
    if return_to_tab >= 1 && return_to_tab <= 9 {
        switch_to_tab_number(return_to_tab);
    }

    tabs
}
